package main

import (
	"strings"
	"testing"
)

func TestFallbackRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"fallback","target":"standard.fallback"
	}`)
	fallback, ok := r.(*FallbackRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *FallbackRule", r)
	}
	if fallback.Target != "standard.fallback" {
		t.Fatalf("fallback fields mismatch: %#v", fallback)
	}
}

func TestFallbackRuleCompileResolvesTarget(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		fallbackRule("standard", "standard.fallback"),
		filterError("standard.fallback", "model_not_found", "5xx"),
		filterProvider("standard.fallback", "c"),
		mapRule("standard.fallback", "native-model"),
		rankRule("standard.fallback"),
		raceRule("standard.fallback", 1),
	)
	route := entryRoute(t, result, "standard")
	if route.Fallback.Target != "standard.fallback" || route.fallbackTarget == nil {
		t.Fatalf("fallback transition mismatch: %#v", route.Fallback)
	}
	if route.fallbackTarget.ModelEq != "" || route.fallbackTarget.Entry {
		t.Fatalf("fallback subroute must not become an entry route")
	}
	if got := targetIDs(route.fallbackTarget.Pool); got != "c" {
		t.Fatalf("fallback subroute pool mismatch: %s", got)
	}
	if !route.fallbackTarget.ErrorIn[ErrorModelNotFound] || !route.fallbackTarget.ErrorIn[ErrorUpstream] || route.fallbackTarget.ErrorIn[ErrorRateLimit] {
		t.Fatalf("fallback error filter mismatch: %#v", route.fallbackTarget.ErrorIn)
	}
}

func TestFallbackRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"fallback","target":"standard.fallback","providers":["a"]
	}`, "unknown field \"providers\"", `action "fallback"`)
}

func TestFallbackRuleDecodeRejectsLegacyStrategy(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"fallback","target":"standard.fallback","fallback_strategy":"race"
	}`, "unknown field \"fallback_strategy\"", `action "fallback"`)
}

func TestFallbackRuleRejectsMissingTarget(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		fallbackRule("standard", ""),
	)...)
	if err == nil || !strings.Contains(err.Error(), "non-empty target") {
		t.Fatalf("expected missing target error, got %v", err)
	}
}

func TestFallbackRuleRequiresRace(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		fallbackRule("standard", "standard.fallback"),
		filterError("standard.fallback", "5xx"),
		filterProvider("standard.fallback", "c"),
		mapRule("standard.fallback", "native-model"),
		rankRule("standard.fallback"),
		raceRule("standard.fallback", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "preceding race") {
		t.Fatalf("expected race prerequisite error for fallback, got %v", err)
	}
}
