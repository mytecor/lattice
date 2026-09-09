package main

import (
	"strings"
	"testing"
)

func TestFallbackRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"match":{"model":"standard"},"action":"fallback",
		"fallback_strategy":"race","on":["model_not_found","429"]
	}`)
	fallback, ok := r.(*FallbackRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *FallbackRule", r)
	}
	if fallback.FallbackStrategy != "race" || len(fallback.On) != 2 {
		t.Fatalf("fallback fields mismatch: %#v", fallback)
	}
}

func TestFallbackRuleCompile(t *testing.T) {
	rules := append(basePipeline("standard"), poolRule("standard", "backup"))
	rules = append(rules, fallbackRule("standard", []string{"model_not_found", "5xx"}, "race"))
	plans, err := compileRules(rules...)
	if err != nil {
		t.Fatal(err)
	}
	fallback := plans["standard"].Fallback
	if fallback == nil || fallback.Mode != "race" {
		t.Fatalf("fallback mismatch: %#v", fallback)
	}
	if got := targetIDs(fallback.Pool); got != "c" {
		t.Fatalf("fallback pool mismatch: %s", got)
	}
	if !fallback.On[ErrorModelNotFound] || !fallback.On[ErrorUpstream] || fallback.On[ErrorRateLimit] {
		t.Fatalf("fallback error filter mismatch: %#v", fallback.On)
	}
}

func TestFallbackRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"fallback",
		"fallback_strategy":"serial","on":["5xx"],"providers":["a"]
	}`, "unknown field \"providers\"", `action "fallback"`)
}

func TestFallbackRuleRejectsDanglingMode(t *testing.T) {
	rules := append(basePipeline("standard"), poolRule("standard", "backup"))
	rules = append(rules, fallbackRule("standard", []string{"5xx"}, "bogus"))
	_, err := compileRules(rules...)
	if err == nil || !strings.Contains(err.Error(), "fallback_strategy") {
		t.Fatalf("expected fallback mode error, got %v", err)
	}
}

func TestFallbackRuleRequiresStageMap(t *testing.T) {
	_, err := compileRules(append(
		basePipeline("standard"),
		fallbackRule("standard", []string{"5xx"}, "serial"),
	)...)
	if err == nil || !strings.Contains(err.Error(), "preceding map") {
		t.Fatalf("expected fallback stage map error, got %v", err)
	}
}
