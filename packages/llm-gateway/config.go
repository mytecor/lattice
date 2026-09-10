package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const defaultCatalogRefresh = 10 * time.Minute

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		d.Duration = 0
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a string such as 500ms or 10m")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value, err)
	}
	d.Duration = parsed
	return nil
}

type Config struct {
	Host                   string       `json:"host"`
	Port                   int          `json:"port"`
	LogLevel               string       `json:"log_level"`
	ClientAPIKey           string       `json:"client_api_key"`
	CatalogRefreshInterval Duration     `json:"catalog_refresh_interval"`
	AffinityFile           string       `json:"affinity_file,omitempty"`
	Providers              []Provider   `json:"providers"`
	RoutingRules           RoutingRules `json:"routing_rules"`
}

type Provider struct {
	ID                  string            `json:"id"`
	BaseProvider        string            `json:"base_provider"`
	InferenceURL        string            `json:"inference_url"`
	ModelsURL           string            `json:"models_url,omitempty"`
	APIKey              string            `json:"api_key,omitempty"`
	ModelsAPIKey        string            `json:"models_api_key,omitempty"`
	Priority            int               `json:"priority,omitempty"`
	Cooldown            Duration          `json:"cooldown,omitempty"`
	RequestTimeout      Duration          `json:"request_timeout,omitempty"`
	BifrostMaxRetries   int               `json:"bifrost_max_retries,omitempty"`
	AllowPrivateNetwork bool              `json:"allow_private_network,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
}

type BackoffConfig struct {
	Type    string   `json:"type"`
	Initial Duration `json:"initial"`
	Max     Duration `json:"max"`
}

// compiledConfig separates external rules from the compiled immutable routing
// graph: named routes, their candidate pools, dispatch batches, explicit
// transition references, semaphore limits and lease and affinity policy.
// Catalog sources are indexed by provider ID: each provider owns its
// last-known-good snapshot and exact native validation.
type compiledConfig struct {
	raw            Config
	logger         *slog.Logger
	providers      map[string]Provider
	routes         map[string]*compiledRoute
	models         map[string]*compiledRoute // logical model → entry route
	logicalIDs     []string
	catalogSources map[string][]catalogSource
}

type catalogSource struct {
	URL      string
	APIKey   string
	Explicit bool
}

// compiledRoute is the compiled, immutable routing-graph node for one named
// route. External Nix/JSON rules are fully normalized into this structure at
// compile time; the runtime scheduler consumes only these immutable routes and
// never touches the JSON DTOs. Target names are resolved to pointers at the
// compile stage, so a missing target or a routing cycle already fails there.
type compiledRoute struct {
	Name string
	// Entry is true when the route carries a request-level model filter: it is
	// a discoverable logical entry route. Subroutes (retry/fallback/hedge
	// targets) are not Entry and never become logical models.
	Entry   bool
	ModelEq string // request-level model filter, non-empty for entry routes
	// ErrorIn gates a transition into this route on the incoming terminal
	// failure class; empty means the route is applicable on any failure.
	ErrorIn map[ErrorClass]bool
	// AttemptLT gates entry into this route on the current attempt number
	// (0 >= AttemptLT blocks the transition). Zero means no bound.
	AttemptLT int
	// ProviderUnused restricts the compiled pool at runtime to providers not
	// yet used by the current request graph (explicit routing policy).
	ProviderUnused bool
	// Pool is the ranked candidate target pairs of the route after the
	// filter/map/rank sequence; an immutable snapshot kept at the race action.
	Pool []Target
	// RaceCount is the size of the route race batch; 0 means all pool
	// candidates.
	RaceCount int
	Lease     LeaseConfig
	Affinity  AffinityConfig
	Retry     RetryConfig
	Fallback  FallbackConfig
	Hedge     HedgeConfig
	Semaphore SemaphoreConfig
	// RouteTimeout bounds the entire route graph.
	RouteTimeout time.Duration

	// Resolved transitions (compile stage).
	retryTarget    *compiledRoute
	fallbackTarget *compiledRoute
	hedgeTarget    *compiledRoute
}

// applicable evaluates the destination-owned applicability of the route for an
// incoming terminal failure at the given attempt index. An empty error filter
// applies to any failure; a failure matching no class makes the route not
// applicable, which must never mask the original terminal failure.
func (rt *compiledRoute) applicable(callErr *CallError, attempt int) bool {
	if callErr == nil {
		return true
	}
	if len(rt.ErrorIn) > 0 && !rt.ErrorIn[callErr.Class] {
		return false
	}
	if rt.AttemptLT > 0 && attempt >= rt.AttemptLT {
		return false
	}
	return true
}

// RetryConfig is a bounded repeated transition to a named route. The retry
// condition lives in the target route's own filter; here only the lifecycle
// (attempts, backoff) plus the unresolved target name at decode time.
type RetryConfig struct {
	Target   string // resolved to retryTarget at compile stage
	Attempts int
	Backoff  BackoffConfig
}

// FallbackConfig is a one-shot transition to a named route.
type FallbackConfig struct {
	Target string // resolved to fallbackTarget at compile stage
}

// HedgeConfig is a latency transition: after the delay the target route's
// batch may start while the current route is still executing.
type HedgeConfig struct {
	After  time.Duration
	Target string // resolved to hedgeTarget at compile stage
}

type SemaphoreConfig struct {
	MaxCalls            int
	MaxInFlight         int
	MaxCallsPerProvider int
}

type LeaseConfig struct {
	Enabled                bool
	Source                 string // "winner"
	Duration               time.Duration
	RenewOnSuccess         bool
	ReleaseOn              map[ErrorClass]bool
	ReleaseAfterSlowStarts int
	SlowStart              time.Duration
}

type AffinityConfig struct {
	Enabled           bool
	Sources           []string
	TTL               time.Duration
	OnMissing         string // "ignore"
	OnProviderFailure string // "fail-closed"
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("decode config: trailing JSON content")
	}
	if err := resolveConfigSecrets(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func resolveConfigSecrets(cfg *Config) error {
	var err error
	cfg.ClientAPIKey, err = resolveSecret(cfg.ClientAPIKey)
	if err != nil {
		return fmt.Errorf("client_api_key: %w", err)
	}
	for i := range cfg.Providers {
		cfg.Providers[i].APIKey, err = resolveSecret(cfg.Providers[i].APIKey)
		if err != nil {
			return fmt.Errorf("provider %q api_key: %w", cfg.Providers[i].ID, err)
		}
		cfg.Providers[i].ModelsAPIKey, err = resolveSecret(cfg.Providers[i].ModelsAPIKey)
		if err != nil {
			return fmt.Errorf("provider %q models_api_key: %w", cfg.Providers[i].ID, err)
		}
	}
	return nil
}

func resolveSecret(value string) (string, error) {
	if !strings.HasPrefix(value, "env.") {
		return value, nil
	}
	name := strings.TrimPrefix(value, "env.")
	if name == "" {
		return "", errors.New("empty environment variable name")
	}
	secret, ok := os.LookupEnv(name)
	if !ok || secret == "" {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}
	return secret, nil
}

func compileConfig(cfg Config) (*compiledConfig, error) {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 9208
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	if cfg.CatalogRefreshInterval.Duration == 0 {
		cfg.CatalogRefreshInterval.Duration = defaultCatalogRefresh
	}
	if cfg.CatalogRefreshInterval.Duration < time.Second {
		return nil, fmt.Errorf("catalog_refresh_interval must be at least 1s")
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "silent"
	}
	switch cfg.LogLevel {
	case "silent", "error", "warn", "info", "debug", "trace":
	default:
		return nil, fmt.Errorf("unsupported log_level %q", cfg.LogLevel)
	}
	if len(cfg.Providers) == 0 {
		return nil, errors.New("at least one provider is required")
	}

	compiled := &compiledConfig{
		raw:            cfg,
		logger:         newGatewayLogger(cfg.LogLevel),
		providers:      make(map[string]Provider, len(cfg.Providers)),
		catalogSources: make(map[string][]catalogSource),
	}
	for _, provider := range cfg.Providers {
		provider.ID = strings.TrimSpace(provider.ID)
		provider.BaseProvider = strings.TrimSpace(provider.BaseProvider)
		provider.InferenceURL = strings.TrimRight(strings.TrimSpace(provider.InferenceURL), "/")
		provider.ModelsURL = strings.TrimSpace(provider.ModelsURL)
		if provider.ID == "" || provider.InferenceURL == "" {
			return nil, errors.New("every provider requires id and inference_url")
		}
		if err := validateEndpoint(provider.InferenceURL); err != nil {
			return nil, fmt.Errorf("provider %q inference_url: %w", provider.ID, err)
		}
		if provider.ModelsURL != "" {
			if err := validateEndpoint(provider.ModelsURL); err != nil {
				return nil, fmt.Errorf("provider %q models_url: %w", provider.ID, err)
			}
		}
		if provider.BaseProvider == "" {
			provider.BaseProvider = "openai"
		}
		if _, exists := compiled.providers[provider.ID]; exists {
			return nil, fmt.Errorf("duplicate provider id %q", provider.ID)
		}
		if provider.RequestTimeout.Duration == 0 {
			provider.RequestTimeout.Duration = 60 * time.Second
		}
		if provider.Cooldown.Duration == 0 {
			provider.Cooldown.Duration = 15 * time.Second
		}
		compiled.providers[provider.ID] = provider
		// Discovery is provider-scoped: each provider contributes exactly one
		// catalog source (explicit models_url or the inferred
		// openai-convention <inference_url>/models), and snapshots are indexed
		// by provider ID.
		if provider.ModelsURL != "" {
			compiled.catalogSources[provider.ID] = appendCatalogSource(
				compiled.catalogSources[provider.ID],
				catalogSource{URL: provider.ModelsURL, APIKey: provider.ModelsAPIKey, Explicit: true},
			)
		} else if provider.BaseProvider == "openai" {
			// inference_url is the complete OpenAI-compatible base path. Appending
			// only /models therefore produces /v1/models for a conventional base
			// and preserves arbitrary routed prefixes without duplicating /v1.
			compiled.catalogSources[provider.ID] = appendCatalogSource(
				compiled.catalogSources[provider.ID],
				catalogSource{URL: provider.InferenceURL + "/models", APIKey: provider.APIKey},
			)
		}
	}

	result, err := compileRoutes(cfg.RoutingRules, compiled.providers)
	if err != nil {
		return nil, err
	}
	if len(result.models) == 0 {
		return nil, errors.New("at least one logical model with an entry route is required")
	}
	compiled.routes = result.routes
	compiled.models = result.entries
	compiled.logicalIDs = result.models
	return compiled, nil
}

func appendCatalogSource(sources []catalogSource, candidate catalogSource) []catalogSource {
	for index, source := range sources {
		if source.URL != candidate.URL || source.APIKey != candidate.APIKey {
			continue
		}
		if candidate.Explicit && !source.Explicit {
			sources[index].Explicit = true
		}
		return sources
	}
	return append(sources, candidate)
}

func validateEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return errors.New("invalid URL")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("URL must use http or https and include a host")
	}
	if parsed.User != nil {
		return errors.New("URL must not contain credentials")
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func parseErrorClasses(values []string) map[ErrorClass]bool {
	if len(values) == 0 {
		return allRetryableClasses()
	}
	classes := make(map[ErrorClass]bool, len(values))
	for _, value := range values {
		classes[ErrorClass(strings.ToLower(strings.TrimSpace(value)))] = true
	}
	return classes
}

func allRetryableClasses() map[ErrorClass]bool {
	return map[ErrorClass]bool{
		ErrorTimeout: true, ErrorConnection: true, ErrorRateLimit: true,
		ErrorNotFound: true, ErrorUpstream: true, ErrorInvalid: true,
	}
}
