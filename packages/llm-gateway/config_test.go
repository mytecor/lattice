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
		{ID: "c", Name: "backup", BaseProvider: "openai", InferenceURL: "https://c.invalid", Priority: 5},
	}
	var mapping ModelMapping
	mapping.Match.Provider = "group"
	mapping.Match.ID = "native-model"
	mapping.Override.ID = "standard"
	backupMapping := mapping
	backupMapping.Match.Provider = "backup"
	cfg.Models = []ModelMapping{mapping, backupMapping}
	var race RoutingRule
	race.Match.Model = "standard"
	race.Action = "race"
	race.AccessGroups = []string{"group"}
	var retry RoutingRule
	retry.Match.Model = "standard"
	retry.Action = "retry"
	retry.Attempts = 2
	retry.On = []string{"429", "5xx"}
	retry.Backoff = &BackoffConfig{Type: "exponential", Initial: Duration{100 * time.Millisecond}, Max: Duration{time.Second}}
	var fallback RoutingRule
	fallback.Match.Model = "standard"
	fallback.Action = "fallback"
	fallback.AccessGroups = []string{"backup"}
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

func TestCompileConfigAddsHedgeToPreviousRoute(t *testing.T) {
	cfg := testConfig()
	var hedge RoutingRule
	hedge.Match.Model = "standard"
	hedge.Action = "hedge"
	cfg.RoutingRules = append([]RoutingRule{cfg.RoutingRules[0], hedge}, cfg.RoutingRules[1:]...)
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stage := compiled.plans["standard"].Stages[0]
	if !stage.RetryHedge {
		t.Fatalf("hedge was not compiled into the previous route: %#v", stage)
	}
	if stage.Retries != 2 || !stage.RetryOn[ErrorRateLimit] {
		t.Fatalf("retry no longer composes independently with hedge: %#v", stage)
	}
}

func TestCompileConfigDerivesCatalogURLFromInferenceBase(t *testing.T) {
	cfg := testConfig()
	cfg.Providers[0].InferenceURL = "https://provider.invalid/v1/"
	cfg.Providers[0].APIKey = "provider-a-key"
	cfg.Providers[1].InferenceURL = "https://edge.invalid/functions/v1/gonka"
	cfg.Providers[1].APIKey = "provider-b-key"
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sources := compiled.groupSources["group"]
	if len(sources) != 2 {
		t.Fatalf("expected one implicit catalog per provider, got %#v", sources)
	}
	if sources[0].URL != "https://provider.invalid/v1/models" || sources[0].APIKey != "provider-a-key" {
		t.Fatalf("conventional catalog URL was not derived correctly: %#v", sources[0])
	}
	if sources[1].URL != "https://edge.invalid/functions/v1/gonka/models" || sources[1].APIKey != "provider-b-key" {
		t.Fatalf("custom-prefix catalog URL was not derived correctly: %#v", sources[1])
	}
}

func TestCompileConfigPreservesExplicitCatalogURLAndCredential(t *testing.T) {
	cfg := testConfig()
	cfg.Providers[0].ModelsURL = "https://catalog.invalid/custom/models"
	cfg.Providers[0].ModelsAPIKey = "catalog-key"
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sources := compiled.groupSources["group"]
	if len(sources) != 2 {
		t.Fatalf("unexpected explicit catalogs: %#v", sources)
	}
	var explicit catalogSource
	for _, source := range sources {
		if source.Explicit {
			explicit = source
		}
	}
	if explicit.URL != cfg.Providers[0].ModelsURL || explicit.APIKey != "catalog-key" {
		t.Fatalf("explicit catalog configuration was not preserved: %#v", sources)
	}
}

func TestCompileConfigDoesNotInferCatalogForNonOpenAIAdapter(t *testing.T) {
	cfg := testConfig()
	cfg.Providers[0].BaseProvider = "anthropic"
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range compiled.groupSources["group"] {
		if source.URL == cfg.Providers[0].InferenceURL+"/models" {
			t.Fatalf("OpenAI catalog path was inferred for Anthropic adapter: %#v", source)
		}
	}
}

func TestCompileConfigRejectsAccessGroupWithoutModelMapping(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules[0].AccessGroups = []string{"other-group"}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "no mapping") {
		t.Fatalf("expected mapping error, got %v", err)
	}
}

func TestCompileConfigRejectsAccessGroupsOnModifier(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules[1].AccessGroups = []string{"group"}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "must not declare access_groups") {
		t.Fatalf("expected modifier selector error, got %v", err)
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
