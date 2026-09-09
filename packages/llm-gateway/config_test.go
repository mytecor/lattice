package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rule builds a routing rule for a model with optional field mutations.
func rule(action, model string, modify func(*RoutingRule)) RoutingRule {
	var r RoutingRule
	r.Match.Model = model
	r.Action = action
	if modify != nil {
		modify(&r)
	}
	return r
}

func poolRule(model string, groups ...string) RoutingRule {
	return rule("pool", model, func(r *RoutingRule) { r.AccessGroups = groups })
}

func rankRule(model string) RoutingRule {
	return rule("rank", model, func(r *RoutingRule) { r.Strategy = "priority" })
}

func raceRule(model string, count int) RoutingRule {
	return rule("race", model, func(r *RoutingRule) { r.Count = count })
}

func retryNextRule(model string, count, attempts int) RoutingRule {
	return rule("retry", model, func(r *RoutingRule) {
		r.Scope = "next"
		r.Count = count
		r.Attempts = attempts
		r.On = []string{"429", "5xx"}
		r.Backoff = &BackoffConfig{Type: "exponential", Initial: Duration{100 * time.Millisecond}, Max: Duration{time.Second}}
	})
}

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
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"),
		rankRule("standard"),
		raceRule("standard", 2),
		retryNextRule("standard", 1, 2),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.AccessGroups = []string{"backup"}
			r.On = []string{"timeout", "5xx"}
			r.FallbackStrategy = "serial"
		}),
	}
	return cfg
}

// boundedPipeline returns the canonical f7-09 pipeline for a model.
func boundedPipeline(model string) []RoutingRule {
	return []RoutingRule{
		poolRule(model, "group"),
		rankRule(model),
		rule("lease", model, func(r *RoutingRule) {
			r.Source = "winner"
			r.Duration = Duration{10 * time.Minute}
			r.RenewOnSuccess = boolPtr(true)
			r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
			r.ReleaseAfterSlowStarts = 3
			r.SlowStart = Duration{3 * time.Second}
		}),
		rule("affinity", model, func(r *RoutingRule) {
			r.Sources = []string{"responses.conversation", "responses.previous_response_id"}
			r.TTL = Duration{24 * time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
		raceRule(model, 2),
		retryNextRule(model, 1, 2),
		rule("hedge", model, func(r *RoutingRule) { r.After = Duration{3 * time.Second} }),
		rule("semaphore", model, func(r *RoutingRule) {
			r.MaxCalls = 4
			r.MaxInFlight = 3
			r.MaxCallsPerProvider = 1
		}),
		rule("timeout", model, func(r *RoutingRule) { r.Duration = Duration{60 * time.Second} }),
	}
}

func boolPtr(value bool) *bool { return &value }

func TestCompileConfigBuildsFlatPipeline(t *testing.T) {
	compiled, err := compileConfig(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if got := strings.Join(plan.Pool, ","); got != "a,b" {
		t.Fatalf("priority ranking mismatch: %s", got)
	}
	if plan.RaceCount != 2 {
		t.Fatalf("race count mismatch: %d", plan.RaceCount)
	}
	retry := plan.Retry
	if retry.Scope != "next" || retry.Count != 1 || retry.Attempts != 2 {
		t.Fatalf("retry schedule mismatch: %#v", retry)
	}
	if !retry.On[ErrorRateLimit] || !retry.On[ErrorUpstream] || retry.On[ErrorTimeout] {
		t.Fatalf("retry error filter mismatch: %#v", retry.On)
	}
	if plan.Fallback == nil || !plan.Fallback.On[ErrorTimeout] || !plan.Fallback.On[ErrorUpstream] || plan.Fallback.On[ErrorRateLimit] {
		t.Fatalf("fallback error filter mismatch: %#v", plan.Fallback)
	}
	if compiled.raw.CatalogRefreshInterval.Duration != 10*time.Minute {
		t.Fatalf("catalog refresh default changed: %s", compiled.raw.CatalogRefreshInterval.Duration)
	}
	if compiled.raw.LogLevel != "silent" {
		t.Fatalf("log level default changed: %q", compiled.raw.LogLevel)
	}
}

func TestCompileConfigBoundedPipeline(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = boundedPipeline("standard")
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if !plan.Lease.Enabled || plan.Lease.Duration != 10*time.Minute {
		t.Fatalf("lease policy mismatch: %#v", plan.Lease)
	}
	if !plan.Lease.RenewOnSuccess || plan.Lease.ReleaseAfterSlowStarts != 3 || plan.Lease.SlowStart != 3*time.Second {
		t.Fatalf("lease failure policy mismatch: %#v", plan.Lease)
	}
	if !plan.Lease.ReleaseOn[ErrorRateLimit] || !plan.Lease.ReleaseOn[ErrorConnection] || plan.Lease.ReleaseOn[ErrorNotFound] {
		t.Fatalf("lease release_on mismatch: %#v", plan.Lease.ReleaseOn)
	}
	if !plan.Affinity.Enabled || plan.Affinity.TTL != 24*time.Hour || len(plan.Affinity.Sources) != 2 {
		t.Fatalf("affinity policy mismatch: %#v", plan.Affinity)
	}
	if plan.HedgeAfter != 3*time.Second {
		t.Fatalf("hedge delay mismatch: %s", plan.HedgeAfter)
	}
	if plan.Semaphore.MaxCalls != 4 || plan.Semaphore.MaxInFlight != 3 || plan.Semaphore.MaxCallsPerProvider != 1 {
		t.Fatalf("semaphore limits mismatch: %#v", plan.Semaphore)
	}
	if plan.RouteTimeout != 60*time.Second {
		t.Fatalf("route timeout mismatch: %s", plan.RouteTimeout)
	}
}

func TestCompileConfigLegacyRaceAndRetryNormalize(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		rule("race", "standard", func(r *RoutingRule) { r.AccessGroups = []string{"group"} }),
		rule("retry", "standard", func(r *RoutingRule) { r.Attempts = 1 }),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	// Legacy race normalizes to implicit pool(groups) → rank(priority) → race(all).
	if got := strings.Join(plan.Pool, ","); got != "a,b" {
		t.Fatalf("legacy pool ranking mismatch: %s", got)
	}
	if plan.RaceCount != 0 {
		t.Fatalf("legacy race must cover the whole pool: %d", plan.RaceCount)
	}
	// Legacy retry without scope repeats the original selection.
	if plan.Retry.Scope != "same" || plan.Retry.Attempts != 1 {
		t.Fatalf("legacy retry normalization mismatch: %#v", plan.Retry)
	}
}

func TestCompileConfigRejectsParameterlessHedge(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules[:3], rule("hedge", "standard", nil))
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "migration") {
		t.Fatalf("expected precise hedge migration error, got %v", err)
	}
}

func TestCompileConfigRejectsUnexpectedFields(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules[0].Count = 2 // pool may not carry count
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), `action "pool"`) || !strings.Contains(err.Error(), "unexpected field(s): count") {
		t.Fatalf("expected pool field error, got %v", err)
	}
}

func TestCompileConfigRejectsOutOfOrderActions(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"),
		raceRule("standard", 2),
		rankRule("standard"),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "out of pipeline order") {
		t.Fatalf("expected order error, got %v", err)
	}
}

func TestCompileConfigRejectsRaceWithoutPool(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		rule("race", "standard", func(r *RoutingRule) { r.Count = 2 }),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "preceding pool") {
		t.Fatalf("expected pool prerequisite error, got %v", err)
	}
}

func TestCompileConfigRejectsMissingRace(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{poolRule("standard", "group"), rankRule("standard")}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "no race action") {
		t.Fatalf("expected missing race error, got %v", err)
	}
}

func TestCompileConfigRejectsInvalidRetryNext(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules[:3], rule("retry", "standard", func(r *RoutingRule) {
		r.Scope = "next"
		r.Attempts = 1
	}))
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "positive count") {
		t.Fatalf("expected retry count error, got %v", err)
	}
}

func TestCompileConfigRejectsInvalidLease(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"),
		rule("lease", "standard", func(r *RoutingRule) {
			r.Source = "loser"
			r.Duration = Duration{time.Minute}
		}),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), `lease source`) {
		t.Fatalf("expected lease source error, got %v", err)
	}
}

func TestCompileConfigRejectsInvalidAffinity(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"),
		rule("affinity", "standard", func(r *RoutingRule) {
			r.Sources = []string{"chat.messages"}
			r.TTL = Duration{time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "affinity source") {
		t.Fatalf("expected affinity source error, got %v", err)
	}
}

func TestCompileConfigRejectsInvalidSemaphore(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 0
			r.MaxInFlight = 1
			r.MaxCallsPerProvider = 1
		}),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected semaphore error, got %v", err)
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

func TestCompileConfigRejectsFieldsOnLegacyFallback(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules[4].Duration = Duration{time.Second}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), `action "fallback"`) || !strings.Contains(err.Error(), "unexpected field(s): duration") {
		t.Fatalf("expected fallback field error, got %v", err)
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

// TestCompileBoundedPipelineJSON mirrors the JSON the NixOS module generates
// (builtins.toJSON of the typed routing rules): snake_case field names, null
// unset durations, and action-specific field sets. It proves the generated
// public config is accepted by the gateway's strict validator end to end.
func TestCompileBoundedPipelineJSON(t *testing.T) {
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","name":"group","base_provider":"openai","inference_url":"https://a.invalid","priority":20},
    {"id":"b","name":"group","base_provider":"openai","inference_url":"https://b.invalid","priority":10}
  ],
  "models": [
    {"match":{"provider":"group","id":"native-model"},"override":{"id":"standard"}}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"pool","access_groups":["group"]},
    {"match":{"model":"standard"},"action":"rank","strategy":"priority"},
    {"match":{"model":"standard"},"action":"lease",
      "source":"winner","duration":"10m","renew_on_success":true,
      "release_on":["429","5xx","timeout","connection_error"],
      "release_after_slow_starts":3,"slow_start":"3s"},
    {"match":{"model":"standard"},"action":"affinity",
      "sources":["responses.conversation","responses.previous_response_id"],
      "ttl":"24h","on_missing":"ignore","on_provider_failure":"fail-closed"},
    {"match":{"model":"standard"},"action":"race","count":2},
    {"match":{"model":"standard"},"action":"retry",
      "scope":"next","count":1,"attempts":2,
      "on":["429","5xx","timeout","connection_error","invalid_response"],
      "backoff":{"type":"exponential","initial":"200ms","max":"1s"}},
    {"match":{"model":"standard"},"action":"hedge","after":"3s"},
    {"match":{"model":"standard"},"action":"semaphore",
      "max_calls":4,"max_in_flight":3,"max_calls_per_provider":1},
    {"match":{"model":"standard"},"action":"timeout","duration":"60s"}
  ]
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if got := strings.Join(plan.Pool, ","); got != "a,b" {
		t.Fatalf("ranking mismatch: %s", got)
	}
	if plan.RaceCount != 2 || plan.HedgeAfter != 3*time.Second || plan.RouteTimeout != 60*time.Second {
		t.Fatalf("schedule mismatch: %#v", plan)
	}
	if plan.Semaphore.MaxCalls != 4 || plan.Semaphore.MaxInFlight != 3 || plan.Semaphore.MaxCallsPerProvider != 1 {
		t.Fatalf("semaphore mismatch: %#v", plan.Semaphore)
	}
	if plan.Retry.Scope != "next" || plan.Retry.Count != 1 || plan.Retry.Attempts != 2 {
		t.Fatalf("retry mismatch: %#v", plan.Retry)
	}
	if !plan.Lease.Enabled || plan.Lease.Duration != 10*time.Minute || plan.Lease.SlowStart != 3*time.Second {
		t.Fatalf("lease mismatch: %#v", plan.Lease)
	}
	if !plan.Affinity.Enabled || plan.Affinity.TTL != 24*time.Hour {
		t.Fatalf("affinity mismatch: %#v", plan.Affinity)
	}
}

// TestCompileJSONAcceptsEmptyAccessGroupsOnRace mirrors the JSON the NixOS
// module emitted before the pool rewrite: a race rule carrying an explicitly
// empty access_groups, which follows a pool rule that already declared the
// groups. The gateway must accept the inert field instead of failing config
// validation at startup (regression for the llm-gateway crash-loop on
// mytecor-homelab).
func TestCompileJSONAcceptsEmptyAccessGroupsOnRace(t *testing.T) {
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","name":"group","base_provider":"openai","inference_url":"https://a.invalid","priority":20}
  ],
  "models": [
    {"match":{"provider":"group","id":"native-model"},"override":{"id":"standard"}}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"pool","access_groups":["group"]},
    {"match":{"model":"standard"},"action":"rank","strategy":"priority"},
    {"match":{"model":"standard"},"action":"race","count":2,"access_groups":[]}
  ]
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if plan.RaceCount != 2 {
		t.Fatalf("race count mismatch: %d", plan.RaceCount)
	}
}

// TestCompileJSONRejectsNullShorthandField verifies that a null defaulted
// optional field (as emitted by the Nix module) stays inert.
func TestCompileJSONRejectsUnknownPublicField(t *testing.T) {
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","name":"group","base_provider":"openai","inference_url":"https://a.invalid","priority":20}
  ],
  "models": [
    {"match":{"provider":"group","id":"native-model"},"override":{"id":"standard"}}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"pool","access_groups":["group"]},
    {"match":{"model":"standard"},"action":"rank","strategy":"priority"},
    {"match":{"model":"standard"},"action":"race","count":2,"surprise":true}
  ]
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("expected unknown public field rejection, got %v", err)
	}
}

func TestCompileJSONRejectsZeroValuedMisplacedField(t *testing.T) {
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","name":"group","base_provider":"openai","inference_url":"https://a.invalid"}
  ],
  "models": [
    {"match":{"provider":"group","id":"native-model"},"override":{"id":"standard"}}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"pool","access_groups":["group"],"count":0},
    {"match":{"model":"standard"},"action":"rank","strategy":"priority"},
    {"match":{"model":"standard"},"action":"race","count":1}
  ]
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileConfig(cfg); err == nil || !strings.Contains(err.Error(), "unexpected field(s): count") {
		t.Fatalf("expected explicitly present zero field rejection, got %v", err)
	}
}

// TestValidateExternalGeneratedConfig compiles an externally generated runtime
// JSON config (for example the one produced by the NixOS module) when
// LATTICE_GATEWAY_CONFIG points at it. It is skipped otherwise.
func TestValidateExternalGeneratedConfig(t *testing.T) {
	path := os.Getenv("LATTICE_GATEWAY_CONFIG")
	if path == "" {
		t.Skip("LATTICE_GATEWAY_CONFIG not set")
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load generated config: %v", err)
	}
	if _, err := compileConfig(cfg); err != nil {
		t.Fatalf("compile generated config: %v", err)
	}
}
