package main

import (
	"strings"
	"testing"
	"time"
)

// balancePipeline returns filter provider → map → rank → balance → race.
func balancePipeline(r Rule) []Rule {
	return []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b", "c"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		r,
		raceRule("standard", 1),
	}
}

func TestBalanceRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"balance",
		"strategy":"adaptive","weights":{"a":2,"b":1},
		"window":"5m","error_budget":0.2
	}`)
	balanced, ok := r.(*BalanceRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *BalanceRule", r)
	}
	if balanced.Strategy != "adaptive" {
		t.Fatalf("strategy mismatch: %#v", balanced.Strategy)
	}
	if balanced.Weights["a"] != 2 || balanced.Weights["b"] != 1 {
		t.Fatalf("weights mismatch: %#v", balanced.Weights)
	}
	if balanced.Window.Duration != 5*time.Minute || balanced.ErrorBudget != 0.2 {
		t.Fatalf("window/error_budget mismatch: %#v", balanced)
	}
}

func TestBalanceRuleCompileDefaults(t *testing.T) {
	result := mustCompile(t, balancePipeline(
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "adaptive" }),
	)...)
	balance := entryRoute(t, result, "standard").Balance
	if !balance.Enabled || balance.Strategy != "adaptive" {
		t.Fatalf("balance policy mismatch: %#v", balance)
	}
	if balance.Window != 5*time.Minute {
		t.Fatalf("default window must be 5m, got %v", balance.Window)
	}
	if balance.ErrorBudget != 0.2 {
		t.Fatalf("default error_budget must be 0.2, got %v", balance.ErrorBudget)
	}
}

func TestBalanceRuleCompileP2C(t *testing.T) {
	result := mustCompile(t, balancePipeline(
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "p2c" }),
	)...)
	balance := entryRoute(t, result, "standard").Balance
	if !balance.Enabled || balance.Strategy != "p2c" {
		t.Fatalf("p2c balance policy mismatch: %#v", balance)
	}
	if balance.Window != 5*time.Minute || balance.ErrorBudget != 0.2 {
		t.Fatalf("p2c must inherit the default window/error_budget: %#v", balance)
	}
}

func TestBalanceRuleCompilePreservesWeights(t *testing.T) {
	result := mustCompile(t, balancePipeline(
		balanceRule("standard", func(r *BalanceRule) {
			r.Strategy = "round_robin"
			r.Weights = map[string]int{"a": 1, "c": 4}
		}),
	)...)
	balance := entryRoute(t, result, "standard").Balance
	if balance.Weights["a"] != 1 || balance.Weights["c"] != 4 {
		t.Fatalf("weights mismatch: %#v", balance.Weights)
	}
	if _, ok := balance.Weights["b"]; ok {
		t.Fatalf("unset weight was materialized: %#v", balance.Weights)
	}
}

func TestBalanceRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"balance",
		"strategy":"adaptive","duration":"10m"
	}`, "unknown field \"duration\"", `action "balance"`)
}

func TestBalanceRuleRejectsUnknownStrategy(t *testing.T) {
	_, err := compileRules(balancePipeline(
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "magic" }),
	)...)
	if err == nil || !strings.Contains(err.Error(), "balance strategy") {
		t.Fatalf("expected balance strategy error, got %v", err)
	}
}

func TestBalanceRuleRejectsUnknownWeightProvider(t *testing.T) {
	_, err := compileRules(balancePipeline(
		balanceRule("standard", func(r *BalanceRule) {
			r.Strategy = "weighted"
			r.Weights = map[string]int{"ghost": 3}
		}),
	)...)
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}

func TestBalanceRuleRejectsZeroWeight(t *testing.T) {
	_, err := compileRules(balancePipeline(
		balanceRule("standard", func(r *BalanceRule) {
			r.Strategy = "weighted"
			r.Weights = map[string]int{"a": 0}
		}),
	)...)
	if err == nil || !strings.Contains(err.Error(), "weight for") {
		t.Fatalf("expected weight error, got %v", err)
	}
}

func TestBalanceRuleRejectsAfterRace(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "adaptive" }),
	)
	if err == nil || !strings.Contains(err.Error(), "must precede the race") {
		t.Fatalf("expected balance-after-race error, got %v", err)
	}
}

func TestBalanceRuleRejectsWithLease(t *testing.T) {
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		leaseRule("standard", func(r *LeaseRule) {
			r.Source = "winner"
			r.Duration = Duration{time.Minute}
		}),
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "adaptive" }),
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected balance+lease conflict error, got %v", err)
	}
}

func TestBalanceRuleExplorationRate(t *testing.T) {
	// 1. expected-ttft gets default 0.10 exploration rate
	res1 := mustCompile(t, balancePipeline(
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "expected-ttft" }),
	)...)
	b1 := entryRoute(t, res1, "standard").Balance
	if b1.ExplorationRate != 0.10 {
		t.Fatalf("expected default exploration rate 0.10 for expected-ttft, got %v", b1.ExplorationRate)
	}

	// 2. Explicit exploration rate overrides default
	r2 := decodeRuleJSON(t, `{
		"route":"standard","action":"balance",
		"strategy":"expected-ttft",
		"exploration_rate":0.25
	}`)
	res2 := mustCompile(t, balancePipeline(r2)...)
	b2 := entryRoute(t, res2, "standard").Balance
	if b2.ExplorationRate != 0.25 {
		t.Fatalf("expected exploration rate 0.25, got %v", b2.ExplorationRate)
	}

	// 3. Explicit 0.0 exploration rate disables exploration
	r3 := decodeRuleJSON(t, `{
		"route":"standard","action":"balance",
		"strategy":"expected-ttft",
		"exploration_rate":0.0
	}`)
	res3 := mustCompile(t, balancePipeline(r3)...)
	b3 := entryRoute(t, res3, "standard").Balance
	if b3.ExplorationRate != 0.0 {
		t.Fatalf("expected exploration rate 0.0, got %v", b3.ExplorationRate)
	}
}
