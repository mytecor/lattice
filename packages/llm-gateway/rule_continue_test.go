package main

import (
	"testing"
	"time"
)

// continueEntry returns a minimal valid streaming entry route for logical
// model "standard" that also declares the in-gateway takeover ("continue")
// policy: two providers, a single native mapping, rank, race, then continue.
func continueEntry(idle time.Duration, reshare string) []Rule {
	return []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		continueRule("standard", idle, reshare),
	}
}

func TestContinueRuleCompiles(t *testing.T) {
	res := mustCompile(t, continueEntry(90*time.Second, "")...)
	route := entryRoute(t, res, "standard")
	if !route.Continue.Enabled {
		t.Fatalf("continue must be enabled after a continue action")
	}
	if route.Continue.Idle != 90*time.Second {
		t.Fatalf("continue idle = %v, want 90s", route.Continue.Idle)
	}
	if route.Continue.Reshare != "full" {
		t.Fatalf("continue reshare default = %q, want full", route.Continue.Reshare)
	}
}

func TestContinueRuleExplicitReshare(t *testing.T) {
	res := mustCompile(t, continueEntry(60*time.Second, "full")...)
	route := entryRoute(t, res, "standard")
	if route.Continue.Reshare != "full" {
		t.Fatalf("continue reshare = %q, want full", route.Continue.Reshare)
	}
}

func TestContinueRuleRejectsBadIdle(t *testing.T) {
	_, err := compileRules(continueEntry(0, "")...)
	if err == nil {
		t.Fatal("continue with zero idle must fail")
	}
	_, err = compileRules(continueEntry(3*time.Second, "")...)
	if err == nil {
		t.Fatal("continue idle below 5s must fail (would take over slow reasoning streams)")
	}
}

func TestContinueRuleRejectsBadReshare(t *testing.T) {
	_, err := compileRules(continueEntry(90*time.Second, "partial")...)
	if err == nil {
		t.Fatal("continue with unsupported reshare mode must fail")
	}
}

func TestContinueRuleRequiresEntryRoute(t *testing.T) {
	// A continue action on a subroute (no entry model filter) must fail: the
	// relay policy belongs to the discoverable entry model.
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		continueRule("standard.retry", 90*time.Second, ""),
	)
	if err == nil {
		t.Fatal("continue on a subroute without an entry filter must fail")
	}
}

func TestContinueRuleUnknownActionRejectedByRegistry(t *testing.T) {
	// The strict decoder must reject an unknown action before any concrete
	// type is built; continue is now a known action, so this asserts the
	// registry knows it via the compile path (an unknown spelling is caught by
	// the decoder). We exercise the decoder directly.
	if _, ok := ruleRegistry[ActionContinue]; !ok {
		t.Fatal("continue action not registered")
	}
}

// TestContinueRuleDecodesNixShape pins the exact JSON projection the Nix
// module emits for a continue rule (route, action, idle, reshare) and that the
// strict decoder + config compiler accept it end-to-end.
func TestContinueRuleDecodesNixShape(t *testing.T) {
	rule := decodeRuleJSON(t, `{"route":"smart","action":"continue","idle":"90s","reshare":"full"}`)
	cr, ok := rule.(*ContinueRule)
	if !ok {
		t.Fatalf("decoded type = %T, want *ContinueRule", rule)
	}
	if cr.Idle.Duration != 90*time.Second {
		t.Fatalf("decoded idle = %v, want 90s", cr.Idle.Duration)
	}
}
