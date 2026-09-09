package main

import (
	"strings"
	"testing"
)

func TestMapRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"match":{"model":"standard"},"action":"map",
		"native":"deepseek-ai/DeepSeek-V4","providers":["a","b"]
	}`)
	mapped, ok := r.(*MapRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *MapRule", r)
	}
	if mapped.model() != "standard" || mapped.action() != "map" {
		t.Fatalf("identity mismatch: %#v", mapped)
	}
	if mapped.Native != "deepseek-ai/DeepSeek-V4" || len(mapped.Providers) != 2 {
		t.Fatalf("map fields mismatch: %#v", mapped)
	}
}

func TestMapRuleCompileBuildsPendingPool(t *testing.T) {
	plans, err := compileRules(
		mapRule("standard", "deepseek-ai/DeepSeek-V4", "a", "b"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := targetIDs(plans["standard"].Pool); got != "a,b" {
		t.Fatalf("mapped pool mismatch: %s", got)
	}
	if plans["standard"].Pool[0].Model != "deepseek-ai/DeepSeek-V4" {
		t.Fatalf("native id not preserved: %#v", plans["standard"].Pool[0])
	}
}

func TestMapRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"map",
		"native":"deepseek-ai/DeepSeek-V4","providers":["a"],"count":2
	}`, "unknown field \"count\"", `action "map"`)
}

func TestMapRuleRejectsMissingNative(t *testing.T) {
	_, err := compileRules(
		mapRule("standard", "", "a", "b"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "non-empty native model id") {
		t.Fatalf("expected missing native error, got %v", err)
	}
}

func TestMapRuleRejectsMissingProviders(t *testing.T) {
	_, err := compileRules(
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "at least one provider") {
		t.Fatalf("expected missing providers error, got %v", err)
	}
}

func TestMapRuleRejectsUnknownProvider(t *testing.T) {
	_, err := compileRules(
		mapRule("standard", "native-model", "ghost"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}
