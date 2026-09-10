package main

import (
	"strings"
	"testing"
	"time"
)

func TestAffinityRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"affinity",
		"sources":["responses.conversation","responses.previous_response_id"],
		"ttl":"24h","on_missing":"ignore","on_provider_failure":"fail-closed"
	}`)
	affinity, ok := r.(*AffinityRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *AffinityRule", r)
	}
	if len(affinity.Sources) != 2 || affinity.TTL.Duration != 24*time.Hour || affinity.OnMissing != "ignore" {
		t.Fatalf("affinity fields mismatch: %#v", affinity)
	}
}

// affinityRulesPipeline returns filter provider → map → rank → affinity → race.
func affinityRulesPipeline(r Rule) []Rule {
	return []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		r,
		raceRule("standard", 2),
	}
}

func TestAffinityRuleCompile(t *testing.T) {
	result := mustCompile(t, affinityRulesPipeline(
		affinityRule("standard", func(r *AffinityRule) {
			r.Sources = []string{"responses.conversation"}
			r.TTL = Duration{time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
	)...)
	affinity := entryRoute(t, result, "standard").Affinity
	if !affinity.Enabled || affinity.TTL != time.Hour || len(affinity.Sources) != 1 {
		t.Fatalf("affinity policy mismatch: %#v", affinity)
	}
	if affinity.OnMissing != "ignore" || affinity.OnProviderFailure != "fail-closed" {
		t.Fatalf("affinity behaviors mismatch: %#v", affinity)
	}
}

func TestAffinityRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"affinity",
		"sources":["responses.conversation"],"ttl":"24h","max_calls":4
	}`, "unknown field \"max_calls\"", `action "affinity"`)
}

func TestAffinityRuleRejectsUnknownSource(t *testing.T) {
	_, err := compileRules(affinityRulesPipeline(
		affinityRule("standard", func(r *AffinityRule) {
			r.Sources = []string{"chat.messages"}
			r.TTL = Duration{time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
	)...)
	if err == nil || !strings.Contains(err.Error(), "affinity source") {
		t.Fatalf("expected affinity source error, got %v", err)
	}
}

func TestAffinityRuleRejectsZeroTTL(t *testing.T) {
	_, err := compileRules(affinityRulesPipeline(
		affinityRule("standard", func(r *AffinityRule) {
			r.Sources = []string{"responses.conversation"}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
	)...)
	if err == nil || !strings.Contains(err.Error(), "affinity ttl") {
		t.Fatalf("expected affinity ttl error, got %v", err)
	}
}
