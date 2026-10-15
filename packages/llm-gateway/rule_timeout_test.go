package main

import (
	"strings"
	"testing"
	"time"
)

func TestTimeoutRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{"route":"standard","action":"timeout","duration":"60s"}`)
	timeouted, ok := r.(*TimeoutRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *TimeoutRule", r)
	}
	if timeouted.Duration.Duration != 60*time.Second {
		t.Fatalf("timeout duration mismatch: %#v", timeouted)
	}
}

func TestTimeoutRuleCompile(t *testing.T) {
	result := mustCompile(t, append(standardEntry(), timeoutRule("standard", 60*time.Second))...)
	route := entryRoute(t, result, "standard")
	if route.RouteTimeout != 60*time.Second {
		t.Fatalf("route timeout mismatch: %v", route.RouteTimeout)
	}
	if route.StreamIdleTimeout != 60*time.Second {
		t.Fatalf("stream idle timeout should default to duration: %v", route.StreamIdleTimeout)
	}
}

func TestTimeoutRuleExplicitIdleAndContinuePolicy(t *testing.T) {
	r := decodeRuleJSON(t, `{"route":"smart","action":"timeout","duration":"3m","idle":"5m"}`)
	timeouted, ok := r.(*TimeoutRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *TimeoutRule", r)
	}
	if timeouted.Duration.Duration != 3*time.Minute || timeouted.Idle.Duration != 5*time.Minute {
		t.Fatalf("decoded timeout rule mismatch: %#v", timeouted)
	}

	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("smart", "smart"),
		filterProvider("smart", "a"),
		mapRule("smart", "native-model"),
		rankRule("smart"),
		raceRule("smart", 1),
		&RetryRule{ruleBase: ruleBase{Route: "smart", Action: ActionRetry}, Attempts: 2},
		timeouted,
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.providers["a"].RequestTimeout.Duration; got != 3*time.Minute {
		t.Fatalf("provider request_timeout should inherit max route duration 3m, got %v", got)
	}
	runner := newRunner(compiled, newCatalog(compiled), &fakeExecutor{})
	policy, enabled := runner.continuePolicy("smart")
	if !enabled {
		t.Fatal("continue policy must be enabled for internal retry")
	}
	if policy.Idle != 5*time.Minute {
		t.Fatalf("continue policy idle = %v, want 5m", policy.Idle)
	}
	if policy.Wait != 5*time.Minute {
		t.Fatalf("continue policy wait = %v, want 5m", policy.Wait)
	}
	if policy.Retries != 2 {
		t.Fatalf("continue policy retries = %d, want 2", policy.Retries)
	}
}

func TestTimeoutRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"timeout","duration":"60s","native":"x"
	}`, "unknown field \"native\"", `action "timeout"`)
}

func TestTimeoutRuleRejectsZeroDuration(t *testing.T) {
	_, err := compileRules(append(standardEntry(), timeoutRule("standard", 0))...)
	if err == nil || !strings.Contains(err.Error(), "timeout duration") {
		t.Fatalf("expected timeout duration error, got %v", err)
	}
}

func TestTimeoutRuleRejectedOnSubroute(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		retryRule("standard", "standard.retry", 1),
		filterError("standard.retry", "429"),
		filterProvider("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
		timeoutRule("standard.retry", time.Minute),
	)...)
	if err == nil || !strings.Contains(err.Error(), "request-wide") {
		t.Fatalf("expected request-wide subroute rejection, got %v", err)
	}
}
