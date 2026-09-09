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
		{ID: "a", BaseProvider: "openai", InferenceURL: "https://a.invalid", Priority: 20},
		{ID: "b", BaseProvider: "openai", InferenceURL: "https://b.invalid", Priority: 10},
		{ID: "c", BaseProvider: "openai", InferenceURL: "https://c.invalid", Priority: 5},
	}
	cfg.RoutingRules = RoutingRules{
		poolRule("standard", "group"),
		rankRule("standard"),
		raceRule("standard", 2),
		retryNextRule("standard", 1, 2),
		poolRule("standard", "backup"),
		fallbackRule("standard", []string{"timeout", "5xx"}, "serial"),
	}
	return cfg
}

// boundedPipeline returns the canonical f7-09 pipeline for a model.
func boundedPipeline(model string) []Rule {
	lease := leaseRule(model, func(r *LeaseRule) {
		r.Source = "winner"
		r.Duration = Duration{10 * time.Minute}
		r.RenewOnSuccess = boolPtr(true)
		r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
		r.ReleaseAfterSlowStarts = 3
		r.SlowStart = Duration{3 * time.Second}
	})
	affinity := affinityRule(model, func(r *AffinityRule) {
		r.Sources = []string{"responses.conversation", "responses.previous_response_id"}
		r.TTL = Duration{24 * time.Hour}
		r.OnMissing = "ignore"
		r.OnProviderFailure = "fail-closed"
	})
	return []Rule{
		poolRule(model, "group"),
		rankRule(model),
		lease,
		affinity,
		raceRule(model, 2),
		retryNextRule(model, 1, 2),
		hedgeRule(model, 3*time.Second),
		semaphoreRule(model, 4, 3, 1),
		timeoutRule(model, 60*time.Second),
	}
}

func TestCompileConfigBuildsFlatPipeline(t *testing.T) {
	compiled, err := compileConfig(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if got := targetIDs(plan.Pool); got != "a,b" {
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
	if got := targetIDs(plan.Fallback.Pool); got != "c" {
		t.Fatalf("fallback target pool mismatch: %s", got)
	}
	if compiled.raw.CatalogRefreshInterval.Duration != 10*time.Minute {
		t.Fatalf("catalog refresh default changed: %s", compiled.raw.CatalogRefreshInterval.Duration)
	}
	if compiled.raw.LogLevel != "silent" {
		t.Fatalf("log level default changed: %q", compiled.raw.LogLevel)
	}
	if got := strings.Join(compiled.logicalIDs, ","); got != "standard" {
		t.Fatalf("logical model registry mismatch: %q", got)
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
	if plan.Fallback != nil {
		t.Fatalf("bounded pipeline must not carry a default fallback: %#v", plan.Fallback)
	}
}

func TestCompileConfigRetryWithoutScopeDefaultsToSame(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"),
		rankRule("standard"),
		raceRule("standard", 2),
		retryRule("standard", func(r *RetryRule) { r.Attempts = 1 }),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if got := targetIDs(plan.Pool); got != "a,b" {
		t.Fatalf("mapped pool ranking mismatch: %s", got)
	}
	if plan.RaceCount != 2 {
		t.Fatalf("race count mismatch: %d", plan.RaceCount)
	}
	if plan.Retry.Scope != "same" || plan.Retry.Attempts != 1 {
		t.Fatalf("retry without scope must repeat the original selection: %#v", plan.Retry)
	}
}

func TestCompileConfigConsecutiveMapsAccumulateAndDeduplicate(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		mapRule("standard", "native-model", "a"),
		mapRule("standard", "native-model", "b"),
		rankRule("standard"),
		raceRule("standard", 2),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := targetIDs(compiled.plans["standard"].Pool); got != "a,b" {
		t.Fatalf("consecutive maps must accumulate the pending pool: %s", got)
	}
}

func TestCompileConfigRejectsDuplicateProviderInPool(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		mapRule("standard", "deepseek-ai/model", "a", "a"),
		rankRule("standard"),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "more than once in the pending pool") {
		t.Fatalf("expected duplicate provider error, got %v", err)
	}
}

func TestCompileConfigRejectsUnknownProvider(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		mapRule("standard", "native-model", "ghost"),
		rankRule("standard"),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}

func TestCompileConfigRejectsParameterlessHedge(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules[:3], hedgeRule("standard", 0))
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "migration") {
		t.Fatalf("expected precise hedge migration error, got %v", err)
	}
}

func TestCompileConfigRejectsOutOfOrderActions(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"),
		raceRule("standard", 2),
		rankRule("standard"),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "out of pipeline order") {
		t.Fatalf("expected order error, got %v", err)
	}
}

func TestCompileConfigRejectsMapAfterRank(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"),
		rankRule("standard"),
		poolRule("standard", "group"),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "out of pipeline order") {
		t.Fatalf("expected map-after-rank order error, got %v", err)
	}
}

func TestCompileConfigRejectsRaceWithoutMap(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{raceRule("standard", 2)}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "preceding map") {
		t.Fatalf("expected map prerequisite error, got %v", err)
	}
}

func TestCompileConfigRejectsMissingRace(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{poolRule("standard", "group"), rankRule("standard")}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "no route-creating race action") {
		t.Fatalf("expected missing race error, got %v", err)
	}
}

func TestCompileConfigRejectsDanglingFallbackMap(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules[:3], poolRule("standard", "backup"))
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "dangling map") {
		t.Fatalf("expected dangling map error, got %v", err)
	}
}

func TestCompileConfigRejectsFallbackWithoutPrimaryStage(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		poolRule("standard", "backup"),
		fallbackRule("standard", []string{"5xx"}, "serial"),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "preceding primary race") {
		t.Fatalf("expected fallback stage prerequisite error, got %v", err)
	}
}

func TestCompileConfigRejectsFallbackWithoutStageMap(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"),
		rankRule("standard"),
		raceRule("standard", 2),
		fallbackRule("standard", []string{"5xx"}, "serial"),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "preceding map action that starts the fallback stage") {
		t.Fatalf("expected fallback stage map error, got %v", err)
	}
}

func TestCompileConfigFallbackStageDoesNotMutatePrimary(t *testing.T) {
	cfg := testConfig()
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.plans["standard"]
	if got := targetIDs(plan.Pool); got != "a,b" {
		t.Fatalf("fallback-stage maps must not change the primary pool: %s", got)
	}
	if plan.Fallback == nil {
		t.Fatalf("fallback snapshot missing: %#v", plan.Fallback)
	}
	if got := targetIDs(plan.Fallback.Pool); got != "c" {
		t.Fatalf("fallback snapshot mismatch: %s", got)
	}
}

func TestCompileConfigRejectsInvalidRetryNext(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules[:3],
		retryRule("standard", func(r *RetryRule) {
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
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"), rankRule("standard"),
		leaseRule("standard", func(r *LeaseRule) {
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
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"), rankRule("standard"),
		affinityRule("standard", func(r *AffinityRule) {
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
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
		semaphoreRule("standard", 0, 1, 1),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected semaphore error, got %v", err)
	}
}

func TestCompileConfigModifiersRequireRace(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		poolRule("standard", "group"),
		rankRule("standard"),
		timeoutRule("standard", time.Second),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "timeout requires a preceding race") {
		t.Fatalf("expected race prerequisite error for modifiers, got %v", err)
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
	sources := compiled.catalogSources["a"]
	if len(sources) != 1 {
		t.Fatalf("expected one implicit catalog per provider, got %#v", sources)
	}
	if sources[0].URL != "https://provider.invalid/v1/models" || sources[0].APIKey != "provider-a-key" {
		t.Fatalf("conventional catalog URL was not derived correctly: %#v", sources[0])
	}
	sources = compiled.catalogSources["b"]
	if len(sources) != 1 {
		t.Fatalf("expected one implicit catalog per provider, got %#v", sources)
	}
	if sources[0].URL != "https://edge.invalid/functions/v1/gonka/models" || sources[0].APIKey != "provider-b-key" {
		t.Fatalf("custom-prefix catalog URL was not derived correctly: %#v", sources[0])
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
	sources := compiled.catalogSources["a"]
	if len(sources) != 1 {
		t.Fatalf("unexpected explicit catalogs: %#v", sources)
	}
	if !sources[0].Explicit || sources[0].URL != cfg.Providers[0].ModelsURL || sources[0].APIKey != "catalog-key" {
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
	for _, source := range compiled.catalogSources["a"] {
		if source.URL == cfg.Providers[0].InferenceURL+"/models" {
			t.Fatalf("OpenAI catalog path was inferred for Anthropic adapter: %#v", source)
		}
	}
	if len(compiled.catalogSources["b"]) != 1 {
		t.Fatalf("openai provider without explicit models_url must infer a catalog: %#v", compiled.catalogSources["b"])
	}
}

func TestCompileConfigRejectsDuplicateProviderID(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = append(cfg.Providers, cfg.Providers[0])
	if _, err := compileConfig(cfg); err == nil || !strings.Contains(err.Error(), "duplicate provider id") {
		t.Fatalf("expected duplicate provider error, got %v", err)
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
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid","priority":20},
    {"id":"b","base_provider":"openai","inference_url":"https://b.invalid","priority":10}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"map","native":"native-model","providers":["a","b"]},
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
    {"match":{"model":"standard"},"action":"timeout","duration":"60s"},
    {"match":{"model":"standard"},"action":"map","native":"prefixed/native-model","providers":["a"]},
    {"match":{"model":"standard"},"action":"fallback",
      "fallback_strategy":"race",
      "on":["model_not_found","429","5xx","timeout","connection_error"]}
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
	if got := targetIDs(plan.Pool); got != "a,b" {
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
	if plan.Fallback == nil || plan.Fallback.Mode != "race" || !plan.Fallback.On[ErrorModelNotFound] || !plan.Fallback.On[ErrorRateLimit] {
		t.Fatalf("fallback mismatch: %#v", plan.Fallback)
	}
	if got := targetIDs(plan.Fallback.Pool); got != "a" {
		t.Fatalf("fallback pool mismatch: %s", got)
	}
}

func TestCompileJSONRejectsUnknownPublicField(t *testing.T) {
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid","priority":20}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"map","native":"native-model","providers":["a"]},
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
	// An explicitly-present zero-valued field of another action is a decode
	// error, not silently accepted: count belongs to race/retry, not map.
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid"}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"map","native":"native-model","providers":["a"],"count":0},
    {"match":{"model":"standard"},"action":"rank","strategy":"priority"},
    {"match":{"model":"standard"},"action":"race","count":1}
  ]
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(path)
	if err == nil || !strings.Contains(err.Error(), `unknown field "count"`) || !strings.Contains(err.Error(), `action "map"`) {
		t.Fatalf("expected explicitly present zero foreign field rejection, got %v", err)
	}
}

func TestCompileJSONRejectsUnknownAction(t *testing.T) {
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid"}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"mystery"}
  ]
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported action") || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("expected unknown action rejection, got %v", err)
	}
}

func TestCompileJSONRejectsMissingRequiredRuleField(t *testing.T) {
	// map without providers decodes but fails validation with a positioned
	// cause; the logical model survives into the error.
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid"}
  ],
  "routing_rules": [
    {"match":{"model":"standard"},"action":"map","native":"native-model"},
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
	if _, err := compileConfig(cfg); err == nil || !strings.Contains(err.Error(), "map requires at least one provider id") || !strings.Contains(err.Error(), `model "standard"`) {
		t.Fatalf("expected missing required field error with model context, got %v", err)
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
