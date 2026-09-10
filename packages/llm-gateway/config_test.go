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
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		retryRule("standard", "standard.retry", 1),
		filterError("standard.retry", "429", "5xx", "timeout", "connection_error"),
		filterProviderUnused("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
	}
	return cfg
}

// boundedPipeline returns the canonical bounded route graph for a model: the
// entry route plus explicit retry and hedge subroutes.
func boundedPipeline(model string) []Rule {
	lease := leaseRule("standard", func(r *LeaseRule) {
		r.Source = "winner"
		r.Duration = Duration{10 * time.Minute}
		r.RenewOnSuccess = boolPtr(true)
		r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
		r.ReleaseAfterSlowStarts = 3
		r.SlowStart = Duration{3 * time.Second}
	})
	affinity := affinityRule("standard", func(r *AffinityRule) {
		r.Sources = []string{"responses.conversation", "responses.previous_response_id"}
		r.TTL = Duration{24 * time.Hour}
		r.OnMissing = "ignore"
		r.OnProviderFailure = "fail-closed"
	})
	backoff := &BackoffConfig{
		Type: "exponential", Initial: Duration{200 * time.Millisecond}, Max: Duration{time.Second},
	}
	return []Rule{
		filterModel("standard", model),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		lease,
		affinity,
		raceRule("standard", 2),
		retryRuleBackoff("standard", "standard.retry", 2, backoff),
		hedgeRule("standard", 3*time.Second, "standard.hedge"),
		semaphoreRule("standard", 4, 3, 1),
		timeoutRule("standard", 60*time.Second),
		filterError("standard.retry", "429", "5xx", "timeout", "connection_error", "invalid_response"),
		filterProviderUnused("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
		filterError("standard.hedge", "429", "5xx", "timeout", "connection_error"),
		filterProvider("standard.hedge", "b", "c"),
		mapRule("standard.hedge", "native-model"),
		rankRule("standard.hedge"),
		raceRule("standard.hedge", 1),
	}
}

func TestCompileConfigBuildsFlatPipeline(t *testing.T) {
	compiled, err := compileConfig(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	route := compiled.models["standard"]
	if route == nil {
		t.Fatalf("no entry route for standard: %v", compiled.logicalIDs)
	}
	if got := targetIDs(route.Pool); got != "a,b" {
		t.Fatalf("priority ranking mismatch: %s", got)
	}
	if route.RaceCount != 2 {
		t.Fatalf("race count mismatch: %d", route.RaceCount)
	}
	if route.Retry.Target != "standard.retry" || route.Retry.Attempts != 1 {
		t.Fatalf("retry transition mismatch: %#v", route.Retry)
	}
	if route.retryTarget == nil {
		t.Fatalf("retry target not resolved: %#v", route.Retry)
	}
	if !route.retryTarget.ErrorIn[ErrorRateLimit] || !route.retryTarget.ErrorIn[ErrorUpstream] || !route.retryTarget.ErrorIn[ErrorTimeout] || route.retryTarget.ErrorIn[ErrorModelNotFound] {
		t.Fatalf("retry destination error filter mismatch: %#v", route.retryTarget.ErrorIn)
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
	route := compiled.models["standard"]
	if !route.Lease.Enabled || route.Lease.Duration != 10*time.Minute {
		t.Fatalf("lease policy mismatch: %#v", route.Lease)
	}
	if !route.Lease.RenewOnSuccess || route.Lease.ReleaseAfterSlowStarts != 3 || route.Lease.SlowStart != 3*time.Second {
		t.Fatalf("lease failure policy mismatch: %#v", route.Lease)
	}
	if !route.Lease.ReleaseOn[ErrorRateLimit] || !route.Lease.ReleaseOn[ErrorConnection] || route.Lease.ReleaseOn[ErrorNotFound] {
		t.Fatalf("lease release_on mismatch: %#v", route.Lease.ReleaseOn)
	}
	if !route.Affinity.Enabled || route.Affinity.TTL != 24*time.Hour || len(route.Affinity.Sources) != 2 {
		t.Fatalf("affinity policy mismatch: %#v", route.Affinity)
	}
	if route.Hedge.After != 3*time.Second || route.Hedge.Target != "standard.hedge" {
		t.Fatalf("hedge config mismatch: %#v", route.Hedge)
	}
	if route.Semaphore.MaxCalls != 4 || route.Semaphore.MaxInFlight != 3 || route.Semaphore.MaxCallsPerProvider != 1 {
		t.Fatalf("semaphore limits mismatch: %#v", route.Semaphore)
	}
	if route.RouteTimeout != 60*time.Second {
		t.Fatalf("route timeout mismatch: %s", route.RouteTimeout)
	}
	if route.Fallback.Target != "" || route.fallbackTarget != nil {
		t.Fatalf("bounded pipeline must not carry a fallback")
	}
	if !route.retryTarget.ProviderUnused {
		t.Fatalf("retry subroute must carry the unused-provider routing policy")
	}
}

func TestCompileConfigConsecutiveMapsAccumulateAndDeduplicate(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		filterProvider("standard", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := targetIDs(compiled.models["standard"].Pool); got != "a,b" {
		t.Fatalf("consecutive filter+map pairs must accumulate the pool: %s", got)
	}
}

func TestCompileConfigRejectsDuplicateProviderInPool(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-x"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-y"),
		rankRule("standard"),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected duplicate provider error, got %v", err)
	}
}

func TestCompileConfigRejectsUnknownProvider(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "ghost"),
		mapRule("standard", "native-model"),
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
	cfg.RoutingRules = append(cfg.RoutingRules[:5],
		hedgeRule("standard", 0, "standard.hedge"),
		filterError("standard.hedge", "429"),
		filterProvider("standard.hedge", "b", "c"),
		mapRule("standard.hedge", "native-model"),
		rankRule("standard.hedge"),
		raceRule("standard.hedge", 1),
	)
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "migration") {
		t.Fatalf("expected precise hedge migration error, got %v", err)
	}
}

func TestCompileConfigRejectsMapAfterRank(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		filterProvider("standard", "c"),
		mapRule("standard", "native-model"),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "map must precede rank") {
		t.Fatalf("expected map-after-rank error, got %v", err)
	}
}

func TestCompileConfigRejectsRaceWithoutMap(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "preceding map") {
		t.Fatalf("expected map prerequisite error, got %v", err)
	}
}

func TestCompileConfigRejectsMissingRace(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "route-creating race") {
		t.Fatalf("expected missing race error, got %v", err)
	}
}

func TestCompileConfigRejectsMissingTargetRoute(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules[:5],
		retryRule("standard", "does.not.exist", 2),
	)
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing target error, got %v", err)
	}
}

func TestCompileConfigRejectsOrphanRoute(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules[:5],
		filterProvider("ghost.way", "c"),
		mapRule("ghost.way", "native-model"),
		rankRule("ghost.way"),
		raceRule("ghost.way", 1),
	)
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "not referenced by any transition") {
		t.Fatalf("expected orphan route error, got %v", err)
	}
}

func TestCompileConfigRejectsRoutingCycle(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		retryRule("standard", "standard.retry", 2),
		filterError("standard.retry", "429"),
		filterProvider("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
		fallbackRule("standard.retry", "standard"),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "routing cycle") {
		t.Fatalf("expected routing cycle error, got %v", err)
	}
}

func TestCompileConfigRejectsDuplicateEntryModel(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules,
		filterModel("second", "standard"),
		filterProvider("second", "c"),
		mapRule("second", "native-model"),
		rankRule("second"),
		raceRule("second", 1),
	)
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate entry route") {
		t.Fatalf("expected duplicate entry model error, got %v", err)
	}
}

func TestCompileConfigRejectsMapWithoutSelection(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "preceding filter provider") {
		t.Fatalf("expected missing selection error, got %v", err)
	}
}

func TestCompileConfigSubrouteInheritsRequestWideGuards(t *testing.T) {
	cfg := testConfig()
	// The retry subroute carries its own error filter/provider/map/race but no
	// semaphore/timeout: those come request-wide from the entry route.
	if _, err := compileConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestCompileConfigRejectsInvalidLease(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		leaseRule("standard", func(r *LeaseRule) {
			r.Source = "loser"
			r.Duration = Duration{time.Minute}
		}),
		raceRule("standard", 2),
	}
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "lease source") {
		t.Fatalf("expected lease source error, got %v", err)
	}
}

func TestCompileConfigRejectsInvalidAffinity(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
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
	cfg.RoutingRules = append(cfg.RoutingRules[:5], semaphoreRule("standard", 0, 1, 1))
	_, err := compileConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected semaphore error, got %v", err)
	}
}

func TestCompileConfigModifiersRequireRace(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
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

// TestCompileBoundedPipelineJSON mirrors the JSON the NixOS module generates:
// snake_case field names, null unset durations, and action-specific field sets
// with route/filter/target. It proves the generated public config is accepted
// by the gateway's strict validator end to end.
func TestCompileBoundedPipelineJSON(t *testing.T) {
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid","priority":20},
    {"id":"b","base_provider":"openai","inference_url":"https://b.invalid","priority":10},
    {"id":"c","base_provider":"openai","inference_url":"https://c.invalid","priority":5}
  ],
  "routing_rules": [
    {"route":"standard","action":"filter","where":{"model":{"eq":"standard"}}},
    {"route":"standard","action":"filter","where":{"provider":{"in":["a","b","c"]}}},
    {"route":"standard","action":"map","native":"native-model"},
    {"route":"standard","action":"rank","strategy":"priority"},
    {"route":"standard","action":"lease",
      "source":"winner","duration":"10m","renew_on_success":true,
      "release_on":["429","5xx","timeout","connection_error"],
      "release_after_slow_starts":3,"slow_start":"3s"},
    {"route":"standard","action":"affinity",
      "sources":["responses.conversation","responses.previous_response_id"],
      "ttl":"24h","on_missing":"ignore","on_provider_failure":"fail-closed"},
    {"route":"standard","action":"race","count":2},
    {"route":"standard","action":"retry",
      "target":"standard.retry","attempts":2,
      "backoff":{"type":"exponential","initial":"200ms","max":"1s"}},
    {"route":"standard","action":"hedge","after":"3s","target":"standard.hedge"},
    {"route":"standard","action":"semaphore",
      "max_calls":4,"max_in_flight":3,"max_calls_per_provider":1},
    {"route":"standard","action":"timeout","duration":"60s"},
    {"route":"standard.retry","action":"filter",
      "where":{"error":{"in":["429","5xx","timeout","connection_error","invalid_response"]}}},
    {"route":"standard.retry","action":"filter",
      "where":{"provider":{"in":["b","c"],"unused":true}}},
    {"route":"standard.retry","action":"map","native":"native-model"},
    {"route":"standard.retry","action":"rank","strategy":"priority"},
    {"route":"standard.retry","action":"race","count":1},
    {"route":"standard.hedge","action":"filter",
      "where":{"error":{"in":["429","5xx","timeout","connection_error"]}}},
    {"route":"standard.hedge","action":"filter","where":{"provider":{"in":["b","c"]}}},
    {"route":"standard.hedge","action":"map","native":"native-model"},
    {"route":"standard.hedge","action":"rank","strategy":"priority"},
    {"route":"standard.hedge","action":"race","count":1}
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
	route := compiled.models["standard"]
	if got := targetIDs(route.Pool); got != "a,b,c" {
		t.Fatalf("ranking mismatch: %s", got)
	}
	if route.RaceCount != 2 || route.Hedge.After != 3*time.Second || route.RouteTimeout != 60*time.Second {
		t.Fatalf("schedule mismatch: %#v", route)
	}
	if route.Semaphore.MaxCalls != 4 || route.Semaphore.MaxInFlight != 3 || route.Semaphore.MaxCallsPerProvider != 1 {
		t.Fatalf("semaphore mismatch: %#v", route.Semaphore)
	}
	if route.Retry.Target != "standard.retry" || route.Retry.Attempts != 2 {
		t.Fatalf("retry mismatch: %#v", route.Retry)
	}
	if !route.Lease.Enabled || route.Lease.Duration != 10*time.Minute || route.Lease.SlowStart != 3*time.Second {
		t.Fatalf("lease mismatch: %#v", route.Lease)
	}
	if !route.Affinity.Enabled || route.Affinity.TTL != 24*time.Hour {
		t.Fatalf("affinity mismatch: %#v", route.Affinity)
	}
	retry := route.retryTarget
	if retry == nil || !retry.ErrorIn[ErrorRateLimit] || !retry.ErrorIn[ErrorInvalid] || retry.ErrorIn[ErrorModelNotFound] {
		t.Fatalf("retry destination filter mismatch: %#v", retry.ErrorIn)
	}
	if !retry.ProviderUnused {
		t.Fatalf("retry destination must carry the unused provider policy")
	}
	// Subroutes are never logical models.
	if len(compiled.logicalIDs) != 1 || compiled.logicalIDs[0] != "standard" {
		t.Fatalf("subroutes leaked into the model registry: %v", compiled.logicalIDs)
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
    {"route":"standard","action":"filter","where":{"model":{"eq":"standard"}}},
    {"route":"standard","action":"filter","where":{"provider":{"in":["a"]}}},
    {"route":"standard","action":"map","native":"native-model"},
    {"route":"standard","action":"rank","strategy":"priority"},
    {"route":"standard","action":"race","count":1,"surprise":true}
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
	// error, not silently accepted: count belongs to race, not map.
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid"}
  ],
  "routing_rules": [
    {"route":"standard","action":"filter","where":{"model":{"eq":"standard"}}},
    {"route":"standard","action":"filter","where":{"provider":{"in":["a"]}}},
    {"route":"standard","action":"map","native":"native-model","count":0},
    {"route":"standard","action":"rank","strategy":"priority"},
    {"route":"standard","action":"race","count":1}
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
    {"route":"standard","action":"mystery"}
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
	// map without a preceding provider filter decodes but fails validation
	// with a positioned cause; the named route survives into the error.
	raw := `
{
  "host": "127.0.0.1",
  "port": 9208,
  "providers": [
    {"id":"a","base_provider":"openai","inference_url":"https://a.invalid"}
  ],
  "routing_rules": [
    {"route":"standard","action":"filter","where":{"model":{"eq":"standard"}}},
    {"route":"standard","action":"map","native":"native-model"},
    {"route":"standard","action":"rank","strategy":"priority"},
    {"route":"standard","action":"race","count":1}
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
	if _, err := compileConfig(cfg); err == nil || !strings.Contains(err.Error(), "preceding filter provider") || !strings.Contains(err.Error(), `route "standard"`) {
		t.Fatalf("expected missing selection error with route context, got %v", err)
	}
}

// TestCompileJSONRejectsLegacyFields proves the removed API fails fast with a
// clear decode error instead of being silently migrated: match, map.providers,
// retry.scope, retry.count, retry.on, fallback.on, fallback_strategy.
func TestCompileJSONRejectsLegacyFields(t *testing.T) {
	cases := map[string]string{
		"match": `{"match":{"model":"standard"},"action":"filter","where":{"model":{"eq":"standard"}}}`,
		"map.providers": `{
			"route":"standard","action":"map","native":"x","providers":["a"]}`,
		"retry.scope": `{
			"route":"standard","action":"retry","target":"standard.retry","attempts":1,"scope":"next"}`,
		"retry.count": `{
			"route":"standard","action":"retry","target":"standard.retry","attempts":1,"count":1}`,
		"retry.on": `{
			"route":"standard","action":"retry","target":"standard.retry","attempts":1,"on":["429"]}`,
		"fallback.on": `{
			"route":"standard","action":"fallback","target":"standard.fallback","on":["5xx"]}`,
		"fallback_strategy": `{
			"route":"standard","action":"fallback","target":"standard.fallback","fallback_strategy":"race"}`,
	}
	for name, rule := range cases {
		if _, err := decodeRule([]byte(rule), 0); err == nil {
			t.Fatalf("legacy field %q was silently accepted", name)
		}
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
