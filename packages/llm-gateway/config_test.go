package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	var cfg Config
	cfg.Host = "127.0.0.1"
	cfg.Port = 9208
	cfg.Providers = []Provider{
		{ID: "a", Name: "group", BaseProvider: "openai", InferenceURL: "https://a.invalid", Priority: 20},
		{ID: "b", Name: "group", BaseProvider: "openai", InferenceURL: "https://b.invalid", Priority: 10},
	}
	var mapping ModelMapping
	mapping.Match.Provider = "group"
	mapping.Match.ID = "native-model"
	mapping.Override.ID = "standard"
	cfg.Models = []ModelMapping{mapping}
	var race RoutingRule
	race.Match.Model = "standard"
	race.Action = "race"
	race.Providers = []string{"b", "a"}
	var retry RoutingRule
	retry.Match.Model = "standard"
	retry.Action = "retry"
	retry.Attempts = 2
	retry.On = []string{"429", "5xx"}
	retry.Backoff = &BackoffConfig{Type: "exponential", Initial: Duration{100 * time.Millisecond}, Max: Duration{time.Second}}
	var fallback RoutingRule
	fallback.Match.Model = "standard"
	fallback.Action = "fallback"
	fallback.Providers = []string{"b"}
	fallback.On = []string{"timeout", "5xx"}
	fallback.FallbackStrategy = "serial"
	cfg.RoutingRules = []RoutingRule{race, retry, fallback}
	return cfg
}

func TestCompileConfigBuildsFlatPipeline(t *testing.T) {
	compiled, err := compileConfig(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if len(plan.Stages) != 2 {
		t.Fatalf("expected race plus fallback stages, got %#v", plan.Stages)
	}
	if plan.Stages[0].Mode != "race" || plan.Stages[0].Retries != 2 {
		t.Fatalf("retry did not wrap previous route: %#v", plan.Stages[0])
	}
	if got := strings.Join(plan.Stages[0].Providers, ","); got != "a,b" {
		t.Fatalf("priority ordering mismatch: %s", got)
	}
	if !plan.Stages[0].RetryOn[ErrorRateLimit] || plan.Stages[0].RetryOn[ErrorTimeout] {
		t.Fatalf("retry error filter mismatch: %#v", plan.Stages[0].RetryOn)
	}
	if !plan.Stages[0].NextOn[ErrorTimeout] || plan.Stages[0].NextOn[ErrorRateLimit] {
		t.Fatalf("fallback error filter mismatch: %#v", plan.Stages[0].NextOn)
	}
	if compiled.raw.CatalogRefreshInterval.Duration != 10*time.Minute {
		t.Fatalf("catalog refresh default changed: %s", compiled.raw.CatalogRefreshInterval.Duration)
	}
	if compiled.raw.LogLevel != "silent" {
		t.Fatalf("log level default changed: %q", compiled.raw.LogLevel)
	}
}

func TestCompileConfigRejectsProviderWithoutModelMapping(t *testing.T) {
	cfg := testConfig()
	cfg.Providers[1].Name = "other-group"
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "no mapping") {
		t.Fatalf("expected mapping error, got %v", err)
	}
}

func TestDurationUnmarshal(t *testing.T) {
	var duration Duration
	if err := duration.UnmarshalJSON([]byte(`"250ms"`)); err != nil {
		t.Fatal(err)
	}
	if duration.Duration != 250*time.Millisecond {
		t.Fatalf("unexpected duration %s", duration.Duration)
	}
}

func TestLoadConfigRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}
}

func TestCompileConfigRejectsCredentialsInURL(t *testing.T) {
	cfg := testConfig()
	cfg.Providers[0].InferenceURL = "https://secret@example.invalid"
	if _, err := compileConfig(cfg); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credential URL rejection, got %v", err)
	}
}

func TestCompileConfigRejectsUnknownLogLevel(t *testing.T) {
	cfg := testConfig()
	cfg.LogLevel = "verbose"
	if _, err := compileConfig(cfg); err == nil || !strings.Contains(err.Error(), "log_level") {
		t.Fatalf("expected log level error, got %v", err)
	}
}
