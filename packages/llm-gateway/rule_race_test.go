package main

import (
	"strings"
	"testing"
)

func TestRaceRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{"route":"standard","action":"race","count":2}`)
	raced, ok := r.(*RaceRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *RaceRule", r)
	}
	if raced.Count != 2 {
		t.Fatalf("race count mismatch: %#v", raced)
	}
}

func TestRaceRuleCompileSnapshotsPool(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	)
	route := entryRoute(t, result, "standard")
	if route.RaceCount != 1 || targetIDs(route.Pool) != "a,b" {
		t.Fatalf("race snapshot mismatch: count=%d pool=%s", route.RaceCount, targetIDs(route.Pool))
	}
}

func TestRaceRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"race","count":2,"native":"x"
	}`, "unknown field \"native\"", `action "race"`)
}

func TestRaceRuleRequiresPrecedingMap(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "preceding map") {
		t.Fatalf("expected map prerequisite error, got %v", err)
	}
}

func TestRaceRuleRejectsNegativeCount(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		raceRule("standard", -1),
	)
	if err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("expected negative count error, got %v", err)
	}
}

func TestRaceRuleRejectsCountAbovePool(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds the route candidate pool") {
		t.Fatalf("expected over-capacity count error, got %v", err)
	}
}
