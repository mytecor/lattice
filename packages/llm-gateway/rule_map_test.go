package main

import (
	"strings"
	"testing"
)

func TestMapRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"map","native":"deepseek-ai/DeepSeek-V4"
	}`)
	mapped, ok := r.(*MapRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *MapRule", r)
	}
	if mapped.route() != "standard" || mapped.action() != "map" {
		t.Fatalf("identity mismatch: %#v", mapped)
	}
	if mapped.Native != "deepseek-ai/DeepSeek-V4" {
		t.Fatalf("map fields mismatch: %#v", mapped)
	}
}

func TestMapRuleCompileBuildsPendingPool(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "deepseek-ai/DeepSeek-V4"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	route := entryRoute(t, result, "standard")
	if got := targetIDs(route.Pool); got != "a,b" {
		t.Fatalf("mapped pool mismatch: %s", got)
	}
	if route.Pool[0].Model != "deepseek-ai/DeepSeek-V4" {
		t.Fatalf("native id not preserved: %#v", route.Pool[0])
	}
}

func TestMapRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"map",
		"native":"deepseek-ai/DeepSeek-V4","providers":["a"],"count":2
	}`, "unknown field \"providers\"", `action "map"`)
}

func TestMapRuleRejectsMissingNative(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", ""),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "non-empty native model id") {
		t.Fatalf("expected missing native error, got %v", err)
	}
}

func TestMapRuleRejectsMissingProviderSelection(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "preceding filter provider") {
		t.Fatalf("expected missing provider selection error, got %v", err)
	}
}

func TestMapRuleRejectsSecondMapWithoutFilter(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-x"),
		mapRule("standard", "native-y"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "filter provider action after the previous map") {
		t.Fatalf("expected map-after-map error, got %v", err)
	}
}

func TestMapRuleRejectsUnknownProvider(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "ghost"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}

func TestMapRuleRejectsDuplicateProviderInPool(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-x"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-y"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected duplicate provider error, got %v", err)
	}
}
