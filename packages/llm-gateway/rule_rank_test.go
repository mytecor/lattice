package main

import (
	"strings"
	"testing"
)

func TestRankRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{"match":{"model":"standard"},"action":"rank","strategy":"priority"}`)
	ranked, ok := r.(*RankRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *RankRule", r)
	}
	if ranked.Strategy != "priority" {
		t.Fatalf("rank strategy mismatch: %#v", ranked)
	}
}

func TestRankRuleCompileOrdersByPriority(t *testing.T) {
	plans, err := compileRules(
		mapRule("standard", "native-model", "c", "a", "b"),
		rankRule("standard"),
		raceRule("standard", 3),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := targetIDs(plans["standard"].Pool); got != "a,b,c" {
		t.Fatalf("priority ranking mismatch: %s", got)
	}
}

func TestRankRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"rank","strategy":"priority","count":2
	}`, "unknown field \"count\"", `action "rank"`)
}

func TestRankRuleRejectsUnsupportedStrategy(t *testing.T) {
	r := &RankRule{}
	r.setIdentity("standard", "rank")
	r.Strategy = "random"
	_, err := compileRules(
		mapRule("standard", "native-model", "a"),
		r,
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "rank strategy") {
		t.Fatalf("expected unsupported strategy error, got %v", err)
	}
}

func TestRankRuleRequiresPrecedingMap(t *testing.T) {
	_, err := compileRules(
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "preceding map") {
		t.Fatalf("expected map prerequisite error, got %v", err)
	}
}
