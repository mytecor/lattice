package main

import (
	"strings"
	"testing"
	"time"
)

func TestRetryRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"retry",
		"target":"standard.retry","attempts":2,
		"backoff":{"type":"exponential","initial":"200ms","max":"1s"}
	}`)
	retry, ok := r.(*RetryRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *RetryRule", r)
	}
	if retry.Target != "standard.retry" || retry.Attempts != 2 {
		t.Fatalf("retry fields mismatch: %#v", retry)
	}
	if retry.Backoff == nil || retry.Backoff.Initial.Duration != 200*time.Millisecond {
		t.Fatalf("backoff mismatch: %#v", retry.Backoff)
	}
}

// retryPipeline builds the standard entry route plus a bounded retry subroute
// with the given provider selection and error filter.
func retryPipeline(retryProviders []string, on []string) []Rule {
	rules := append(standardEntry(),
		retryRule("standard", "standard.retry", 1),
	)
	rules = append(rules, filterError("standard.retry", on...))
	rules = append(rules, filterProviderUnused("standard.retry", retryProviders...))
	rules = append(rules, mapRule("standard.retry", "native-model"))
	rules = append(rules, rankRule("standard.retry"), raceRule("standard.retry", 1))
	return rules
}

func TestRetryRuleCompileResolvesTargetAndAppliesBackoffDefaults(t *testing.T) {
	rules := append(standardEntry(),
		retryRuleBackoff("standard", "standard.retry", 2, &BackoffConfig{}),
		filterError("standard.retry", "429"),
		filterProviderUnused("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
	)
	result, err := compileRules(rules...)
	if err != nil {
		t.Fatal(err)
	}
	route := entryRoute(t, result, "standard")
	if route.Retry.Target != "standard.retry" || route.Retry.Attempts != 2 {
		t.Fatalf("retry schedule mismatch: %#v", route.Retry)
	}
	if route.retryTarget == nil || route.retryTarget.Name != "standard.retry" {
		t.Fatalf("retry target not resolved at compile stage")
	}
	if route.Retry.Backoff.Initial.Duration != 100*time.Millisecond || route.Retry.Backoff.Max.Duration != time.Second {
		t.Fatalf("backoff defaults mismatch: %#v", route.Retry.Backoff)
	}
}

func TestRetryRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"retry",
		"target":"standard.retry","attempts":1,"after":"3s"
	}`, "unknown field \"after\"", `action "retry"`)
}

func TestRetryRuleDecodeRejectsLegacyScope(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"retry",
		"target":"standard.retry","attempts":1,"scope":"same"
	}`, "unknown field \"scope\"", `action "retry"`)
}

func TestRetryRuleRejectsZeroAttempts(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		retryRule("standard", "standard.retry", 0),
		filterError("standard.retry", "429"),
		filterProvider("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
	)...)
	if err == nil || !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("expected attempts error, got %v", err)
	}
}

func TestRetryRuleRejectsMissingTarget(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		retryRule("standard", "", 2),
	)...)
	if err == nil || !strings.Contains(err.Error(), "non-empty target") {
		t.Fatalf("expected missing target error, got %v", err)
	}
}

func TestRetryRuleRejectsSelfTarget(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		retryRule("standard", "standard", 2),
	)...)
	if err == nil || !strings.Contains(err.Error(), "must not target the route") {
		t.Fatalf("expected self-target error, got %v", err)
	}
}
