package main

import (
	"strings"
	"testing"
)

func TestRankRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{"route":"standard","action":"rank","strategy":"priority"}`)
	ranked, ok := r.(*RankRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *RankRule", r)
	}
	if ranked.Strategy != "priority" {
		t.Fatalf("rank strategy mismatch: %#v", ranked)
	}
}

func TestRankRuleCompileOrdersByPriority(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "c", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 3),
	)
	route := entryRoute(t, result, "standard")
	if got := targetIDs(route.Pool); got != "a,b,c" {
		t.Fatalf("priority ranking mismatch: %s", got)
	}
}

func TestRankRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"rank","strategy":"priority","count":2
	}`, "unknown field \"count\"", `action "rank"`)
}

func TestRankRuleRejectsUnsupportedStrategy(t *testing.T) {
	r := &RankRule{}
	r.setIdentity("standard", "rank")
	r.Strategy = "random"
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		r,
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "rank strategy") {
		t.Fatalf("expected unsupported strategy error, got %v", err)
	}
}

func TestRankRuleRequiresPrecedingMap(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "preceding map") {
		t.Fatalf("expected map prerequisite error, got %v", err)
	}
}
