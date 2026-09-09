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
func compileRules(rules ...Rule) (map[string]Plan, error) {
	return compilePlans(rules, testProviders)
}

// basePipeline returns the minimal valid primary stage for a model: map → rank
// → race over providers a,b (the "group" pool), ranked by priority.
func basePipeline(model string) []Rule {
	return []Rule{poolRule(model, "group"), rankRule(model), raceRule(model, 2)}
}

// mapRule builds a map action binding one native model to a set of providers.
func mapRule(model, native string, providers ...string) Rule {
	r := &MapRule{}
	r.setIdentity(model, "map")
	r.Native = native
	r.Providers = providers
	return r
}

// poolRule is a test convenience that expands the virtual access-group names of
// the shared provider registry into provider IDs with the shared native model
// id: "group" expands to a,b and "backup" to c.
func poolRule(model string, groups ...string) Rule {
	var providers []string
	for _, group := range groups {
		switch group {
		case "group":
			providers = append(providers, "a", "b")
		case "backup":
			providers = append(providers, "c")
		default:
			providers = append(providers, group)
		}
	}
	return mapRule(model, "native-model", providers...)
}

func rankRule(model string) Rule {
	r := &RankRule{}
	r.setIdentity(model, "rank")
	r.Strategy = "priority"
	return r
}

func raceRule(model string, count int) Rule {
	r := &RaceRule{}
	r.setIdentity(model, "race")
	r.Count = count
	return r
}

func retryNextRule(model string, count, attempts int) Rule {
	r := &RetryRule{}
	r.setIdentity(model, "retry")
	r.Scope = "next"
	r.Count = count
	r.Attempts = attempts
	r.On = []string{"429", "5xx"}
	r.Backoff = &BackoffConfig{Type: "exponential", Initial: Duration{100 * time.Millisecond}, Max: Duration{time.Second}}
	return r
}

// retryRule builds a retry action whose fields are set by modify.
func retryRule(model string, modify func(*RetryRule)) Rule {
	r := &RetryRule{}
	r.setIdentity(model, "retry")
	if modify != nil {
		modify(r)
	}
	return r
}

func leaseRule(model string, modify func(*LeaseRule)) Rule {
	r := &LeaseRule{}
	r.setIdentity(model, "lease")
	if modify != nil {
		modify(r)
	}
	return r
}

func affinityRule(model string, modify func(*AffinityRule)) Rule {
	r := &AffinityRule{}
	r.setIdentity(model, "affinity")
	if modify != nil {
		modify(r)
	}
	return r
}

func hedgeRule(model string, after time.Duration) Rule {
	r := &HedgeRule{}
	r.setIdentity(model, "hedge")
	r.After = Duration{after}
	return r
}

func semaphoreRule(model string, maxCalls, maxInFlight, maxCallsPerProvider int) Rule {
	r := &SemaphoreRule{}
	r.setIdentity(model, "semaphore")
	r.MaxCalls = maxCalls
	r.MaxInFlight = maxInFlight
	r.MaxCallsPerProvider = maxCallsPerProvider
	return r
}

func timeoutRule(model string, duration time.Duration) Rule {
	r := &TimeoutRule{}
	r.setIdentity(model, "timeout")
	r.Duration = Duration{duration}
	return r
}

func fallbackRule(model string, on []string, mode string) Rule {
	r := &FallbackRule{}
	r.setIdentity(model, "fallback")
	r.On = on
	r.FallbackStrategy = mode
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
