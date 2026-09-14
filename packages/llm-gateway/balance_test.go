package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// --- balance runtime wiring -------------------------------------------------

// balanceEntryRules builds an entry route with a balance action before race:
// filter model → filter provider → map → rank → balance → race(count=1).
func balanceEntryRules(providers []string) []Rule {
	return []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", providers...),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "round_robin" }),
		raceRule("standard", 1),
	}
}

// TestBalanceRequestServedByBalancedProvider verifies that with race count 1
// the balance action's runtime choice is the only provider executed, and that
// round_robin distributes consecutive requests over the healthy pool.
func TestBalanceRequestServedByBalancedProvider(t *testing.T) {
	compiled := rulesConfig(t, 3, balanceEntryRules([]string{"a", "b", "c"}))
	var mu sync.Mutex
	called := []string{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		called = append(called, target.Provider)
		mu.Unlock()
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	winners := map[string]int{}
	for range 6 {
		body, err := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		winner, _ := payload["winner"].(string)
		winners[winner]++
	}
	mu.Lock()
	defer mu.Unlock()
	if len(called) != 6 {
		t.Fatalf("expected exactly one provider per request, got %#v", called)
	}
	if len(winners) < 2 {
		t.Fatalf("round_robin must distribute over the pool, got %#v", winners)
	}
}

// TestBalanceExcludesUnhealthyProvider verifies that a provider over its error
// budget is not balanced to the front while the adaptive strategy is active.
func TestBalanceExcludesUnhealthyProvider(t *testing.T) {
	compiled := rulesConfig(t, 3, []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b", "c"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		balanceRule("standard", func(r *BalanceRule) {
			r.Strategy = "adaptive"
			r.Window = Duration{Duration: 5 * time.Minute}
			r.ErrorBudget = 0.2
		}),
		raceRule("standard", 1),
	})
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	// Pre-populate the health store: a and b are over the error budget, c is
	// healthy. The adaptive strategy must then narrow the balance choice to c.
	for _, provider := range []string{"a", "b"} {
		for range 4 {
			runner.scores.Observe(provider, &CallError{Class: ErrorUpstream, Status: 503}, 0)
		}
		runner.scores.Observe(provider, nil, time.Second)
	}
	// Now c must be the only healthy provider and get balanced to the front.
	seen := map[string]int{}
	for range 4 {
		body, err := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		seen[payload["winner"].(string)]++
	}
	if len(seen) != 1 || seen["c"] != 4 {
		t.Fatalf("unhealthy providers must be excluded, got %#v", seen)
	}
}
