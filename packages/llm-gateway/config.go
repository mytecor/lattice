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
	Host                   string        `json:"host"`
	Port                   int           `json:"port"`
	LogLevel               string        `json:"log_level"`
	ClientAPIKey           string        `json:"client_api_key"`
	CatalogRefreshInterval Duration      `json:"catalog_refresh_interval"`
	AffinityFile           string        `json:"affinity_file,omitempty"`
	Providers              []Provider    `json:"providers"`
	RoutingRules           []RoutingRule `json:"routing_rules"`
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

// RoutingRule is one entry of the flat routing pipeline. Each action accepts
// only its own fields; validation rejects unknown or misplaced fields.
type RoutingRule struct {
	Match struct {
		Model string `json:"model"`
	} `json:"match"`
	Action string `json:"action"`

	// map
	Native    string   `json:"native,omitempty"`
	Providers []string `json:"providers,omitempty"`
	// rank
	Strategy string `json:"strategy,omitempty"`
	// lease
	Source                 string   `json:"source,omitempty"`
	Duration               Duration `json:"duration,omitempty"`
	RenewOnSuccess         *bool    `json:"renew_on_success,omitempty"`
	ReleaseOn              []string `json:"release_on,omitempty"`
	ReleaseAfterSlowStarts int      `json:"release_after_slow_starts,omitempty"`
	SlowStart              Duration `json:"slow_start,omitempty"`
	// affinity
	Sources           []string `json:"sources,omitempty"`
	TTL               Duration `json:"ttl,omitempty"`
	OnMissing         string   `json:"on_missing,omitempty"`
	OnProviderFailure string   `json:"on_provider_failure,omitempty"`
	// race / retry
	Count    int            `json:"count,omitempty"`
	Scope    string         `json:"scope,omitempty"`
	Attempts int            `json:"attempts,omitempty"`
	On       []string       `json:"on,omitempty"`
	Backoff  *BackoffConfig `json:"backoff,omitempty"`
	// hedge / fallback
	After Duration `json:"after,omitempty"`
	// semaphore
	MaxCalls            int `json:"max_calls,omitempty"`
	MaxInFlight         int `json:"max_in_flight,omitempty"`
	MaxCallsPerProvider int `json:"max_calls_per_provider,omitempty"`
	// fallback
	FallbackStrategy string `json:"fallback_strategy,omitempty"`

	// presentFields records fields explicitly present at the JSON boundary so
	// strict per-action validation can distinguish omission from zero values.
	presentFields map[string]bool
}

// UnmarshalJSON preserves field presence while retaining strict rejection of
// fields that are unknown to the routing-rule envelope.
func (r *RoutingRule) UnmarshalJSON(data []byte) error {
	type plainRoutingRule RoutingRule
	var decoded plainRoutingRule
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = RoutingRule(decoded)
	r.presentFields = make(map[string]bool, len(fields))
	for name := range fields {
		if name != "match" && name != "action" {
			r.presentFields[name] = true
		}
	}
	return nil
}

type BackoffConfig struct {
	Type    string   `json:"type"`
	Initial Duration `json:"initial"`
	Max     Duration `json:"max"`
}

// Routing action names. The canonical pipeline order is map → rank → lease →
// affinity → race → retry → hedge → semaphore → timeout; fallback is a
// terminal route-creating action of an optional second stage. One or more
// consecutive map actions build the pending candidate pool of ready target
// pairs (provider ID, native model); rank transforms it and the route-creating
// action (race, or fallback for the second stage) keeps an immutable snapshot.
const (
	ActionMap       = "map"
	ActionRank      = "rank"
	ActionLease     = "lease"
	ActionAffinity  = "affinity"
	ActionRace      = "race"
	ActionRetry     = "retry"
	ActionHedge     = "hedge"
	ActionSemaphore = "semaphore"
	ActionTimeout   = "timeout"
	ActionFallback  = "fallback"
)

// actionRank returns the canonical pipeline position of an action.
func actionRank(action string) int {
	switch action {
	case ActionMap:
		return 1
	case ActionRank:
		return 2
	case ActionLease:
		return 3
	case ActionAffinity:
		return 4
	case ActionRace:
		return 5
	case ActionRetry:
		return 6
	case ActionHedge:
		return 7
	case ActionSemaphore:
		return 8
	case ActionTimeout:
		return 9
	case ActionFallback:
		return 10
	}
	return 0
}

// compiledConfig separates external rules from the compiled candidate pool,
// ranking, dispatch batches, retry/hedge schedule, semaphore limits and lease
// and affinity policy. Catalog sources are indexed by provider ID: each
// provider owns its last-known-good snapshot and exact native validation.
type compiledConfig struct {
	raw            Config
	logger         *slog.Logger
	providers      map[string]Provider
	plans          map[string]Plan
	logicalIDs     []string
	catalogSources map[string][]catalogSource
}

type catalogSource struct {
	URL      string
	APIKey   string
	Explicit bool
}

// Plan is the compiled, immutable routing contract for one logical model.
// External Nix/JSON rules are fully normalized here; the runtime scheduler
// consumes only this structure. Pool elements are ready target pairs
// (provider ID, native model) produced by map rules and ranked by rank.
type Plan struct {
	LogicalModel string
	// Pool is the ranked candidate target pairs of the primary stage after
	// map() and rank(); it is an immutable snapshot kept at the race action.
	Pool []Target
	// RaceCount is the size of the initial race batch; 0 means all pool
	// candidates.
	RaceCount int
	Lease     LeaseConfig
	Affinity  AffinityConfig
	Retry     RetryConfig
	// HedgeAfter is the delay after which the next retry batch may start even
	// though the current branches have not completed.
	HedgeAfter time.Duration
	Semaphore  SemaphoreConfig
	// RouteTimeout bounds the entire compiled route.
	RouteTimeout time.Duration
	// Fallback is the optional terminal fallback route of a second stage: an
	// immutable target-pool snapshot plus the error classes that transition
	// from the primary stage.
	Fallback *FallbackRoute
}

type RetryConfig struct {
	// Scope is "same" (repeat the original race selection) or "next" (use the
	// next unused ranked targets).
	Scope    string
	Count    int // batch size for scope=next; 0 falls back to the race count.
	Attempts int
	On       map[ErrorClass]bool
	Backoff  BackoffConfig
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

type FallbackRoute struct {
	Pool []Target
	Mode string // "serial" | "race" | "hedge"
	On   map[ErrorClass]bool
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
		plans:          make(map[string]Plan),
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

	plans, err := compilePlans(cfg.RoutingRules, compiled.providers)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, errors.New("at least one logical model with routing rules is required")
	}
	for _, id := range sortedKeys(plans) {
		compiled.logicalIDs = append(compiled.logicalIDs, id)
	}
	compiled.plans = plans
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

// present reports which RoutingRule fields are explicitly set.
func (r RoutingRule) present() map[string]bool {
	p := make(map[string]bool, len(r.presentFields))
	for name := range r.presentFields {
		p[name] = true
	}
	if r.Native != "" {
		p["native"] = true
	}
	if len(r.Providers) > 0 {
		p["providers"] = true
	}
	if r.Strategy != "" {
		p["strategy"] = true
	}
	if r.Source != "" {
		p["source"] = true
	}
	if r.Duration.Duration != 0 {
		p["duration"] = true
	}
	if r.RenewOnSuccess != nil {
		p["renew_on_success"] = true
	}
	if len(r.ReleaseOn) > 0 {
		p["release_on"] = true
	}
	if r.ReleaseAfterSlowStarts != 0 {
		p["release_after_slow_starts"] = true
	}
	if r.SlowStart.Duration != 0 {
		p["slow_start"] = true
	}
	if len(r.Sources) > 0 {
		p["sources"] = true
	}
	if r.TTL.Duration != 0 {
		p["ttl"] = true
	}
	if r.OnMissing != "" {
		p["on_missing"] = true
	}
	if r.OnProviderFailure != "" {
		p["on_provider_failure"] = true
	}
	if r.Count != 0 {
		p["count"] = true
	}
	if r.Scope != "" {
		p["scope"] = true
	}
	if r.Attempts != 0 {
		p["attempts"] = true
	}
	if len(r.On) > 0 {
		p["on"] = true
	}
	if r.Backoff != nil {
		p["backoff"] = true
	}
	if r.After.Duration != 0 {
		p["after"] = true
	}
	if r.MaxCalls != 0 {
		p["max_calls"] = true
	}
	if r.MaxInFlight != 0 {
		p["max_in_flight"] = true
	}
	if r.MaxCallsPerProvider != 0 {
		p["max_calls_per_provider"] = true
	}
	if r.FallbackStrategy != "" {
		p["fallback_strategy"] = true
	}
	return p
}

// forbidden returns explicitly set fields that are not allowed for the action.
func (r RoutingRule) forbidden(allow ...string) []string {
	allowed := make(map[string]bool, len(allow))
	for _, name := range allow {
		allowed[name] = true
	}
	var extra []string
	for name, set := range r.present() {
		if set && !allowed[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return extra
}

func ruleErrf(index int, model, action, format string, args ...any) error {
	return fmt.Errorf("routing rule %d (model %q, action %q): %s",
		index, model, action, fmt.Sprintf(format, args...))
}

// stageState tracks the per-model compilation of the pending candidate pool
// across the primary and optional fallback stages.
type stageState struct {
	pending          []Target
	sawMap           bool
	ranked           bool
	primaryRace      bool
	inFallback       bool
	fallbackDeclared bool
	lastRank         int
}

func (st *stageState) beginFallbackStage() {
	st.pending = nil
	st.sawMap = false
	st.ranked = false
	st.inFallback = true
	st.lastRank = 1
}

// compilePlans validates the flat pipeline and produces the compiled per-model
// Plan. Errors carry the rule index, model, action and the concrete cause.
// One or more consecutive map actions form the pending candidate pool; rank
// transforms it, and the route-creating action (race for the primary stage,
// fallback for the optional second stage) keeps an immutable snapshot. A
// provider may appear in a pending pool only once; a new map set after race
// belongs to the fallback stage and never changes the compiled primary stage.
func compilePlans(rules []RoutingRule, providers map[string]Provider) (map[string]Plan, error) {
	plans := make(map[string]Plan)
	states := make(map[string]*stageState)
	for index, rule := range rules {
		model := strings.TrimSpace(rule.Match.Model)
		if model == "" {
			return nil, fmt.Errorf("routing rule %d has no match.model", index)
		}
		action := strings.ToLower(strings.TrimSpace(rule.Action))
		if action == "" {
			return nil, fmt.Errorf("routing rule %d (model %q) has no action", index, model)
		}
		rank := actionRank(action)
		if rank == 0 {
			return nil, fmt.Errorf("routing rule %d (model %q) has unsupported action %q", index, model, action)
		}
		st := states[model]
		if st == nil {
			st = &stageState{}
			states[model] = st
		}
		plan := plans[model]
		plan.LogicalModel = model

		// A map after the primary race begins the optional fallback stage.
		if action == ActionMap && st.primaryRace && !st.fallbackDeclared && !st.inFallback {
			st.beginFallbackStage()
		} else if !st.inFallback && rank < st.lastRank {
			return nil, ruleErrf(index, model, action, "actions out of pipeline order: %q must not follow an action at position %d", action, st.lastRank)
		} else if st.inFallback && action != ActionMap && action != ActionRank && action != ActionFallback {
			return nil, ruleErrf(index, model, action, "only map, rank and fallback are allowed in the fallback stage")
		}

		switch action {
		case ActionMap:
			if st.fallbackDeclared {
				return nil, ruleErrf(index, model, action, "map is not allowed after a declared fallback stage")
			}
			if st.ranked {
				return nil, ruleErrf(index, model, action, "map must precede rank within a stage")
			}
			if extra := rule.forbidden("native", "providers"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if strings.TrimSpace(rule.Native) == "" {
				return nil, ruleErrf(index, model, action, "map requires a non-empty native model id")
			}
			if len(rule.Providers) == 0 {
				return nil, ruleErrf(index, model, action, "map requires at least one provider id")
			}
			seen := make(map[string]struct{}, len(st.pending))
			for _, target := range st.pending {
				seen[target.Provider] = struct{}{}
			}
			for _, providerID := range rule.Providers {
				providerID = strings.TrimSpace(providerID)
				if _, exists := providers[providerID]; !exists {
					return nil, ruleErrf(index, model, action, "map references unknown provider %q", providerID)
				}
				if _, duplicate := seen[providerID]; duplicate {
					return nil, ruleErrf(index, model, action, "provider %q appears more than once in the pending pool", providerID)
				}
				seen[providerID] = struct{}{}
				st.pending = append(st.pending, Target{Provider: providerID, Model: strings.TrimSpace(rule.Native)})
			}
			st.sawMap = true
		case ActionRank:
			if extra := rule.forbidden("strategy"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.sawMap {
				return nil, ruleErrf(index, model, action, "rank requires a preceding map action")
			}
			if st.ranked {
				return nil, ruleErrf(index, model, action, "rank already declared for this stage")
			}
			if rule.Strategy != "priority" {
				return nil, ruleErrf(index, model, action, "unsupported rank strategy %q (only \"priority\" is implemented)", rule.Strategy)
			}
			sort.SliceStable(st.pending, func(i, j int) bool {
				left, right := providers[st.pending[i].Provider], providers[st.pending[j].Provider]
				if left.Priority != right.Priority {
					return left.Priority > right.Priority
				}
				return st.pending[i].Provider < st.pending[j].Provider
			})
			st.ranked = true
		case ActionLease:
			if extra := rule.forbidden("source", "duration", "renew_on_success", "release_on", "release_after_slow_starts", "slow_start"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.sawMap {
				return nil, ruleErrf(index, model, action, "lease requires a preceding map action")
			}
			if rule.Source != "winner" {
				return nil, ruleErrf(index, model, action, "unsupported lease source %q (only \"winner\")", rule.Source)
			}
			if rule.Duration.Duration <= 0 {
				return nil, ruleErrf(index, model, action, "lease duration must be positive")
			}
			lease := LeaseConfig{Enabled: true, Source: "winner", Duration: rule.Duration.Duration, RenewOnSuccess: true}
			if rule.RenewOnSuccess != nil {
				lease.RenewOnSuccess = *rule.RenewOnSuccess
			}
			lease.ReleaseOn = parseErrorClasses(rule.ReleaseOn)
			if rule.ReleaseAfterSlowStarts > 0 {
				if rule.SlowStart.Duration <= 0 {
					return nil, ruleErrf(index, model, action, "release_after_slow_starts requires a positive slow_start")
				}
				lease.ReleaseAfterSlowStarts = rule.ReleaseAfterSlowStarts
				lease.SlowStart = rule.SlowStart.Duration
			}
			plan.Lease = lease
		case ActionAffinity:
			if extra := rule.forbidden("sources", "ttl", "on_missing", "on_provider_failure"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.sawMap {
				return nil, ruleErrf(index, model, action, "affinity requires a preceding map action")
			}
			if len(rule.Sources) == 0 {
				return nil, ruleErrf(index, model, action, "affinity requires at least one source")
			}
			for _, source := range rule.Sources {
				switch source {
				case "responses.conversation", "responses.previous_response_id":
				default:
					return nil, ruleErrf(index, model, action, "unsupported affinity source %q", source)
				}
			}
			if rule.TTL.Duration <= 0 {
				return nil, ruleErrf(index, model, action, "affinity ttl must be positive")
			}
			if rule.OnMissing != "ignore" {
				return nil, ruleErrf(index, model, action, "unsupported on_missing %q (only \"ignore\")", rule.OnMissing)
			}
			if rule.OnProviderFailure != "fail-closed" {
				return nil, ruleErrf(index, model, action, "unsupported on_provider_failure %q (only \"fail-closed\")", rule.OnProviderFailure)
			}
			plan.Affinity = AffinityConfig{
				Enabled: true, Sources: append([]string(nil), rule.Sources...),
				TTL: rule.TTL.Duration, OnMissing: "ignore", OnProviderFailure: "fail-closed",
			}
		case ActionRace:
			if extra := rule.forbidden("count"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if st.primaryRace {
				return nil, ruleErrf(index, model, action, "race already declared for this model")
			}
			if !st.sawMap {
				return nil, ruleErrf(index, model, action, "race requires a preceding map action")
			}
			if rule.Count < 0 {
				return nil, ruleErrf(index, model, action, "race count must not be negative")
			}
			// The route-creating action keeps an immutable snapshot of the
			// pending pool; later fallback maps never change it.
			plan.Pool = append([]Target(nil), st.pending...)
			plan.RaceCount = rule.Count
			st.primaryRace = true
		case ActionRetry:
			if extra := rule.forbidden("scope", "count", "attempts", "on", "backoff"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.primaryRace {
				return nil, ruleErrf(index, model, action, "retry requires a preceding race action")
			}
			scope := rule.Scope
			if scope == "" {
				scope = "same"
			}
			if scope != "same" && scope != "next" {
				return nil, ruleErrf(index, model, action, "unsupported retry scope %q (only \"same\" and \"next\")", scope)
			}
			if rule.Attempts < 1 {
				return nil, ruleErrf(index, model, action, "retry attempts must be at least 1")
			}
			retry := RetryConfig{Scope: scope, Attempts: rule.Attempts, On: parseErrorClasses(rule.On)}
			if scope == "next" && rule.Count < 1 {
				return nil, ruleErrf(index, model, action, "retry scope \"next\" requires a positive count (batch size)")
			}
			retry.Count = rule.Count
			if rule.Backoff != nil {
				retry.Backoff = *rule.Backoff
			}
			if retry.Backoff.Initial.Duration == 0 {
				retry.Backoff.Initial.Duration = 100 * time.Millisecond
			}
			if retry.Backoff.Max.Duration == 0 {
				retry.Backoff.Max.Duration = time.Second
			}
			plan.Retry = retry
		case ActionHedge:
			if extra := rule.forbidden("after"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.primaryRace {
				return nil, ruleErrf(index, model, action, "hedge requires a preceding race action")
			}
			if rule.After.Duration <= 0 {
				return nil, ruleErrf(index, model, action, "hedge requires a positive after duration (migration from f7-09: parameterless hedge is no longer supported; add after=<delay>)")
			}
			plan.HedgeAfter = rule.After.Duration
		case ActionSemaphore:
			if extra := rule.forbidden("max_calls", "max_in_flight", "max_calls_per_provider"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.primaryRace {
				return nil, ruleErrf(index, model, action, "semaphore requires a preceding race action")
			}
			if rule.MaxCalls < 1 || rule.MaxInFlight < 1 || rule.MaxCallsPerProvider < 1 {
				return nil, ruleErrf(index, model, action, "max_calls, max_in_flight and max_calls_per_provider must all be positive")
			}
			plan.Semaphore = SemaphoreConfig{
				MaxCalls: rule.MaxCalls, MaxInFlight: rule.MaxInFlight, MaxCallsPerProvider: rule.MaxCallsPerProvider,
			}
		case ActionTimeout:
			if extra := rule.forbidden("duration"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.primaryRace {
				return nil, ruleErrf(index, model, action, "timeout requires a preceding race action")
			}
			if rule.Duration.Duration <= 0 {
				return nil, ruleErrf(index, model, action, "timeout duration must be positive")
			}
			plan.RouteTimeout = rule.Duration.Duration
		case ActionFallback: // terminal action of the optional second stage
			if extra := rule.forbidden("on", "fallback_strategy", "after"); len(extra) != 0 {
				return nil, ruleErrf(index, model, action, "unexpected field(s): %s", strings.Join(extra, ", "))
			}
			if !st.primaryRace {
				return nil, ruleErrf(index, model, action, "fallback requires a preceding primary race stage")
			}
			if !st.inFallback || !st.sawMap {
				return nil, ruleErrf(index, model, action, "fallback requires a preceding map action that starts the fallback stage")
			}
			if plan.Fallback != nil || st.fallbackDeclared {
				return nil, ruleErrf(index, model, action, "fallback already declared for this model")
			}
			mode := rule.FallbackStrategy
			if mode == "" {
				mode = "serial"
			}
			if mode != "serial" && mode != "race" && mode != "hedge" {
				return nil, ruleErrf(index, model, action, "unsupported fallback_strategy %q", mode)
			}
			plan.Fallback = &FallbackRoute{
				Pool: append([]Target(nil), st.pending...),
				Mode: mode,
				On:   parseErrorClasses(rule.On),
			}
			st.fallbackDeclared = true
		}
		st.lastRank = rank
		plans[model] = plan
	}

	for model, plan := range plans {
		if len(plan.Pool) == 0 {
			return nil, fmt.Errorf("logical model %q has no route-creating race action", model)
		}
		if !states[model].primaryRace {
			return nil, fmt.Errorf("logical model %q has no race action", model)
		}
		if states[model].inFallback && !states[model].fallbackDeclared {
			return nil, fmt.Errorf("logical model %q has a dangling map without a route-creating fallback action", model)
		}
		if plan.LogicalModel == "" {
			plan.LogicalModel = model
		}
		plans[model] = plan
	}
	return plans, nil
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
