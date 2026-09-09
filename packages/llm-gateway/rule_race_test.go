package main

import (
	"strings"
	"testing"
)

func TestRaceRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{"match":{"model":"standard"},"action":"race","count":2}`)
	raced, ok := r.(*RaceRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *RaceRule", r)
	}
	if raced.Count != 2 {
		t.Fatalf("race count mismatch: %#v", raced)
	}
}

func TestRaceRuleCompileSnapshotsPool(t *testing.T) {
	plans, err := compileRules(
		poolRule("standard", "group"),
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan := plans["standard"]
	if plan.RaceCount != 1 || targetIDs(plan.Pool) != "a,b" {
		t.Fatalf("race snapshot mismatch: count=%d pool=%s", plan.RaceCount, targetIDs(plan.Pool))
	}
}

func TestRaceRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"race","count":2,"native":"x"
	}`, "unknown field \"native\"", `action "race"`)
}

func TestRaceRuleRequiresPrecedingMap(t *testing.T) {
	_, err := compileRules(raceRule("standard", 2))
	if err == nil || !strings.Contains(err.Error(), "preceding map") {
		t.Fatalf("expected map prerequisite error, got %v", err)
	}
}

func TestRaceRuleRejectsNegativeCount(t *testing.T) {
	_, err := compileRules(poolRule("standard", "group"), rankRule("standard"), raceRule("standard", -1))
	if err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("expected negative count error, got %v", err)
	}
}
