package main

import (
	"strings"
	"testing"
	"time"
)

// testProviders is the shared provider registry used by compile-level unit
// tests. Priorities order a > b > c.
var testProviders = map[string]Provider{
	"a": {ID: "a", Priority: 20},
	"b": {ID: "b", Priority: 10},
	"c": {ID: "c", Priority: 5},
}

// compileRules compiles a flat rule list against the shared provider registry.
func compileRules(rules ...Rule) (*routeCompileResult, error) {
	return compileRoutes(rules, testProviders)
}

// mustCompile compiles the rules and fails the test on error.
func mustCompile(t *testing.T, rules ...Rule) *routeCompileResult {
	t.Helper()
	result, err := compileRules(rules...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// entryRoute returns the entry route of the compiled result for model.
func entryRoute(t *testing.T, result *routeCompileResult, model string) *compiledRoute {
	t.Helper()
	route := result.entries[model]
	if route == nil {
		t.Fatalf("no entry route for model %q (models: %v)", model, result.models)
	}
	return route
}

// standardEntry returns the minimal valid entry route for logical model
// "standard": route "standard" with an entry model filter, the a,b provider
// selection, one native mapping, ranking and a race of 2.
func standardEntry() []Rule {
	return []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	}
}

func filterModel(route, model string) Rule {
	r := &FilterRule{}
	r.setIdentity(route, "filter")
	r.Where.Model = &filterModelCond{Eq: model}
	return r
}

func filterProvider(route string, in ...string) Rule {
	r := &FilterRule{}
	r.setIdentity(route, "filter")
	r.Where.Provider = &filterProviderCond{In: &in}
	return r
}

func filterProviderUnused(route string, in ...string) Rule {
	r := &FilterRule{}
	r.setIdentity(route, "filter")
	r.Where.Provider = &filterProviderCond{In: &in, Unused: true}
	return r
}

func filterProviderNotIn(route string, notIn ...string) Rule {
	r := &FilterRule{}
	r.setIdentity(route, "filter")
	r.Where.Provider = &filterProviderCond{NotIn: notIn}
	return r
}

func filterError(route string, classes ...string) Rule {
	r := &FilterRule{}
	r.setIdentity(route, "filter")
	r.Where.Error = &filterErrorCond{In: classes}
	return r
}

func filterAttempt(route string, lt int) Rule {
	r := &FilterRule{}
	r.setIdentity(route, "filter")
	r.Where.Attempt = &filterAttemptCond{Lt: lt}
	return r
}

// mapRule builds a map action binding the current provider selection to the
// native model id.
func mapRule(route, native string) Rule {
	r := &MapRule{}
	r.setIdentity(route, "map")
	r.Native = native
	return r
}

func rankRule(route string) Rule {
	r := &RankRule{}
	r.setIdentity(route, "rank")
	r.Strategy = "priority"
	return r
}

func raceRule(route string, count int) Rule {
	r := &RaceRule{}
	r.setIdentity(route, "race")
	r.Count = count
	return r
}

func retryRule(route, target string, attempts int) Rule {
	r := &RetryRule{}
	r.setIdentity(route, "retry")
	r.Target = target
	r.Attempts = attempts
	return r
}

func retryRuleBackoff(route, target string, attempts int, backoff *BackoffConfig) Rule {
	r := &RetryRule{}
	r.setIdentity(route, "retry")
	r.Target = target
	r.Attempts = attempts
	r.Backoff = backoff
	return r
}

func fallbackRule(route, target string) Rule {
	r := &FallbackRule{}
	r.setIdentity(route, "fallback")
	r.Target = target
	return r
}

func hedgeRule(route string, after time.Duration, target string) Rule {
	r := &HedgeRule{}
	r.setIdentity(route, "hedge")
	r.After = Duration{after}
	r.Target = target
	return r
}

func leaseRule(route string, modify func(*LeaseRule)) Rule {
	r := &LeaseRule{}
	r.setIdentity(route, "lease")
	if modify != nil {
		modify(r)
	}
	return r
}

func affinityRule(route string, modify func(*AffinityRule)) Rule {
	r := &AffinityRule{}
	r.setIdentity(route, "affinity")
	if modify != nil {
		modify(r)
	}
	return r
}

func semaphoreRule(route string, maxCalls, maxInFlight, maxCallsPerProvider int) Rule {
	r := &SemaphoreRule{}
	r.setIdentity(route, "semaphore")
	r.MaxCalls = maxCalls
	r.MaxInFlight = maxInFlight
	r.MaxCallsPerProvider = maxCallsPerProvider
	return r
}

func timeoutRule(route string, duration time.Duration) Rule {
	r := &TimeoutRule{}
	r.setIdentity(route, "timeout")
	r.Duration = Duration{duration}
	return r
}

func boolPtr(value bool) *bool { return &value }

// targetIDs joins the provider ids of a compiled target pool for assertions.
func targetIDs(targets []Target) string {
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.Provider)
	}
	return strings.Join(ids, ",")
}

// decodeRuleJSON decodes one rule object through the two-phase decoder.
func decodeRuleJSON(t *testing.T, data string) Rule {
	t.Helper()
	rule, err := decodeRule([]byte(data), 0)
	if err != nil {
		t.Fatalf("decode rule: %v", err)
	}
	return rule
}

// decodeRuleError decodes one rule object and expects a decode error.
func decodeRuleError(t *testing.T, data string, contains ...string) error {
	t.Helper()
	_, err := decodeRule([]byte(data), 0)
	if err == nil {
		t.Fatalf("expected decode error for %s", data)
	}
	for _, want := range contains {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("decode error %q does not contain %q", err.Error(), want)
		}
	}
	return err
}
