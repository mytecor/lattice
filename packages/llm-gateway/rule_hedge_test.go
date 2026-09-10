package main

import (
	"strings"
	"testing"
	"time"
)

func TestHedgeRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"hedge","after":"3s","target":"standard.hedge"
	}`)
	hedged, ok := r.(*HedgeRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *HedgeRule", r)
	}
	if hedged.After.Duration != 3*time.Second || hedged.Target != "standard.hedge" {
		t.Fatalf("hedge fields mismatch: %#v", hedged)
	}
}

func TestHedgeRuleCompileResolvesTarget(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		hedgeRule("standard", 3*time.Second, "standard.hedge"),
		filterError("standard.hedge", "429", "5xx"),
		filterProvider("standard.hedge", "b", "c"),
		mapRule("standard.hedge", "native-model"),
		rankRule("standard.hedge"),
		raceRule("standard.hedge", 1),
	)
	route := entryRoute(t, result, "standard")
	if route.Hedge.After != 3*time.Second || route.Hedge.Target != "standard.hedge" {
		t.Fatalf("hedge config mismatch: %#v", route.Hedge)
	}
	if route.hedgeTarget == nil {
		t.Fatalf("hedge target not resolved at compile stage")
	}
	if got := targetIDs(route.hedgeTarget.Pool); got != "b,c" {
		t.Fatalf("hedge subroute pool mismatch: %s", got)
	}
}

func TestHedgeRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"hedge","after":"3s","target":"standard.hedge","count":2
	}`, "unknown field \"count\"", `action "hedge"`)
}

func TestHedgeRuleRejectsZeroAfter(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		hedgeRule("standard", 0, "standard.hedge"),
		filterError("standard.hedge", "429"),
		filterProvider("standard.hedge", "c"),
		mapRule("standard.hedge", "native-model"),
		rankRule("standard.hedge"),
		raceRule("standard.hedge", 1),
	)...)
	if err == nil || !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "migration") {
		t.Fatalf("expected parameterless hedge error, got %v", err)
	}
}

func TestHedgeRuleRejectsMissingTarget(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		hedgeRule("standard", 3*time.Second, ""),
	)...)
	if err == nil || !strings.Contains(err.Error(), "non-empty target") {
		t.Fatalf("expected missing target error, got %v", err)
	}
}
