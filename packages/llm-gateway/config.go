package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Host                   string         `json:"host"`
	Port                   int            `json:"port"`
	ClientAPIKey           string         `json:"client_api_key"`
	CatalogRefreshInterval Duration       `json:"catalog_refresh_interval"`
	Providers              []Provider     `json:"providers"`
	Models                 []ModelMapping `json:"models"`
	RoutingRules           []RoutingRule  `json:"routing_rules"`
}

type Provider struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
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

type ModelMapping struct {
	Match struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
	} `json:"match"`
	Override struct {
		ID string `json:"id"`
	} `json:"override"`
}

type RoutingRule struct {
	Match struct {
		Model string `json:"model"`
	} `json:"match"`
	Action           string         `json:"action"`
	Providers        []string       `json:"providers,omitempty"`
	Attempts         int            `json:"attempts,omitempty"`
	On               []string       `json:"on,omitempty"`
	Backoff          *BackoffConfig `json:"backoff,omitempty"`
	Duration         Duration       `json:"duration,omitempty"`
	After            Duration       `json:"after,omitempty"`
	FallbackStrategy string         `json:"fallback_strategy,omitempty"`
}

type BackoffConfig struct {
	Type    string   `json:"type"`
	Initial Duration `json:"initial"`
	Max     Duration `json:"max"`
}

type compiledConfig struct {
	raw         Config
	providers   map[string]Provider
	mappings    map[string]map[string]string
	plans       map[string]Plan
	logicalIDs  []string
	groupSource map[string]catalogSource
}

type catalogSource struct {
	URL    string
	APIKey string
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
	if len(cfg.Providers) == 0 {
		return nil, errors.New("at least one provider is required")
	}

	compiled := &compiledConfig{
		raw:         cfg,
		providers:   make(map[string]Provider, len(cfg.Providers)),
		mappings:    make(map[string]map[string]string),
		plans:       make(map[string]Plan),
		groupSource: make(map[string]catalogSource),
	}
	for _, provider := range cfg.Providers {
		provider.ID = strings.TrimSpace(provider.ID)
		provider.Name = strings.TrimSpace(provider.Name)
		provider.BaseProvider = strings.TrimSpace(provider.BaseProvider)
		provider.InferenceURL = strings.TrimRight(strings.TrimSpace(provider.InferenceURL), "/")
		provider.ModelsURL = strings.TrimSpace(provider.ModelsURL)
		if provider.ID == "" || provider.Name == "" || provider.InferenceURL == "" {
			return nil, errors.New("every provider requires id, name, and inference_url")
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
		if provider.ModelsURL != "" {
			source := catalogSource{URL: provider.ModelsURL, APIKey: provider.ModelsAPIKey}
			if previous, exists := compiled.groupSource[provider.Name]; exists && previous != source {
				return nil, fmt.Errorf("access group %q has conflicting model catalog sources", provider.Name)
			}
			compiled.groupSource[provider.Name] = source
		}
	}

	logicalSet := make(map[string]struct{})
	for _, mapping := range cfg.Models {
		group := strings.TrimSpace(mapping.Match.Provider)
		nativeID := strings.TrimSpace(mapping.Match.ID)
		logicalID := strings.TrimSpace(mapping.Override.ID)
		if group == "" || nativeID == "" || logicalID == "" {
			return nil, errors.New("every model mapping requires match.provider, match.id, and override.id")
		}
		if compiled.mappings[logicalID] == nil {
			compiled.mappings[logicalID] = make(map[string]string)
		}
		if _, duplicate := compiled.mappings[logicalID][group]; duplicate {
			return nil, fmt.Errorf("logical model %q has more than one primary in group %q", logicalID, group)
		}
		compiled.mappings[logicalID][group] = nativeID
		logicalSet[logicalID] = struct{}{}
	}
	if len(logicalSet) == 0 {
		return nil, errors.New("at least one logical model mapping is required")
	}
	for id := range logicalSet {
		compiled.logicalIDs = append(compiled.logicalIDs, id)
	}
	sort.Strings(compiled.logicalIDs)

	plans, err := compilePlans(cfg.RoutingRules, compiled.providers, compiled.mappings)
	if err != nil {
		return nil, err
	}
	for _, logicalID := range compiled.logicalIDs {
		if _, ok := plans[logicalID]; !ok {
			return nil, fmt.Errorf("logical model %q has no routing rules", logicalID)
		}
	}
	compiled.plans = plans
	return compiled, nil
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
