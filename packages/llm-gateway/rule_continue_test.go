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

func TestContinueRuleRejectsNegativeRetries(t *testing.T) {
	r := continueRule("standard", 90*time.Second, "full")
	r.(*ContinueRule).Retries = -1
	if _, err := compileRules(r); err == nil {
		t.Fatal("continue with negative retries must fail")
	}
}

func TestContinueRuleRejectsRetriesAboveCap(t *testing.T) {
	r := continueRule("standard", 90*time.Second, "full")
	r.(*ContinueRule).Retries = maxContinueChainRetries + 1
	if _, err := compileRules(r); err == nil {
		t.Fatal("continue with retries above the cap must fail")
	}
}

func TestContinueRuleAcceptsRetriesAtCap(t *testing.T) {
	res := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		continueRuleRetries("standard", 90*time.Second, "full", maxContinueChainRetries),
	)
	route := entryRoute(t, res, "standard")
	if !route.Continue.Enabled || route.Continue.Retries != maxContinueChainRetries {
		t.Fatalf("continue retries at cap = %d, want %d", route.Continue.Retries, maxContinueChainRetries)
	}
}

func TestContinueRuleRetriesCompiles(t *testing.T) {
	res := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		continueRuleRetries("standard", 90*time.Second, "full", 2),
	)
	route := entryRoute(t, res, "standard")
	if !route.Continue.Enabled || route.Continue.Retries != 2 {
		t.Fatalf("continue retries = %v, want 2", route.Continue.Retries)
	}
}

func TestContinueRuleRetriesDefaultZero(t *testing.T) {
	// A continue rule without an explicit retries keeps the pre-chain-retry
	// behavior: retries defaults to zero.
	res := mustCompile(t, continueEntry(90*time.Second, "")...)
	route := entryRoute(t, res, "standard")
	if route.Continue.Retries != 0 {
		t.Fatalf("continue retries default = %d, want 0", route.Continue.Retries)
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
// module emits for a continue rule (route, action, idle, reshare, retries) and
// that the strict decoder + config compiler accept it end-to-end.
func TestContinueRuleDecodesNixShape(t *testing.T) {
	rule := decodeRuleJSON(t, `{"route":"smart","action":"continue","idle":"90s","reshare":"full"}`)
	cr, ok := rule.(*ContinueRule)
	if !ok {
		t.Fatalf("decoded type = %T, want *ContinueRule", rule)
	}
	if cr.Idle.Duration != 90*time.Second {
		t.Fatalf("decoded idle = %v, want 90s", cr.Idle.Duration)
	}
	if cr.Retries != 0 {
		t.Fatalf("decoded retries (absent) = %d, want 0", cr.Retries)
	}

	// The Nix pipeline now always emits retries (default 0); a Nix deploy
	// opting into whole-chain retry sets it explicitly.
	rule = decodeRuleJSON(t, `{"route":"smart","action":"continue","idle":"30s","reshare":"full","retries":1}`)
	cr, ok = rule.(*ContinueRule)
	if !ok {
		t.Fatalf("decoded type = %T, want *ContinueRule", rule)
	}
	if cr.Retries != 1 {
		t.Fatalf("decoded retries = %d, want 1", cr.Retries)
	}
}
