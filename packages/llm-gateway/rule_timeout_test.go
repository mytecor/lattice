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
	if entryRoute(t, result, "standard").RouteTimeout != 60*time.Second {
		t.Fatalf("route timeout mismatch")
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
