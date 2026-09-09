package main

import (
	"strings"
	"testing"
	"time"
)

func TestTimeoutRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{"match":{"model":"standard"},"action":"timeout","duration":"60s"}`)
	timeouted, ok := r.(*TimeoutRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *TimeoutRule", r)
	}
	if timeouted.Duration.Duration != 60*time.Second {
		t.Fatalf("timeout duration mismatch: %#v", timeouted)
	}
}

func TestTimeoutRuleCompile(t *testing.T) {
	plans, err := compileRules(append(basePipeline("standard"), timeoutRule("standard", 60*time.Second))...)
	if err != nil {
		t.Fatal(err)
	}
	if plans["standard"].RouteTimeout != 60*time.Second {
		t.Fatalf("route timeout mismatch: %s", plans["standard"].RouteTimeout)
	}
}

func TestTimeoutRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"timeout","duration":"60s","native":"x"
	}`, "unknown field \"native\"", `action "timeout"`)
}

func TestTimeoutRuleRejectsZeroDuration(t *testing.T) {
	_, err := compileRules(append(basePipeline("standard"), timeoutRule("standard", 0))...)
	if err == nil || !strings.Contains(err.Error(), "timeout duration") {
		t.Fatalf("expected timeout duration error, got %v", err)
	}
}
