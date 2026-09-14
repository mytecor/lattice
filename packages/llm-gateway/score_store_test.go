package main

import (
	"math"
	"sync"
	"testing"
	"time"
)

// storeWithClock returns a ScoreStore with an injectable clock and a way to
// advance it, so tests can age the sliding window deterministically.
type storeClock struct {
	store *ScoreStore
	now   time.Time
}

func newStoreAt(now time.Time) *storeClock {
	inst := &storeClock{now: now}
	inst.store = newScoreStore(func() time.Time { return inst.now })
	return inst
}

func (s *storeClock) advance(d time.Duration) { s.now = s.now.Add(d) }

func TestHealthAccounting(t *testing.T) {
	clock := newStoreAt(time.Now())
	// Fresh provider: no evidence yet → healthy.
	if got := clock.store.health("a", 5*time.Minute, 0.2); got != 1 {
		t.Fatalf("fresh provider health = %v, want 1", got)
	}
	// A success with latency starts the EWMA and keeps health at 1.
	clock.store.Observe("a", nil, 1*time.Second)
	if got := clock.store.health("a", 5*time.Minute, 0.2); got != 1 {
		t.Fatalf("healthy provider after one success health = %v, want 1", got)
	}
	// A single health error is inside the budget → health in (0,1): with one
	// error among two samples the error rate is 0.5, so a budget of 0.9 keeps
	// the provider inside (factor 1 - 0.5/0.9 ≈ 0.44) and no other provider has
	// a latency baseline, so the latency factor stays 1.
	clock.store.Observe("a", &CallError{Class: ErrorRateLimit}, 0)
	if got := clock.store.health("a", 5*time.Minute, 0.9); got <= 0 || got >= 1 {
		t.Fatalf("single error under budget health = %v, want (0,1)", got)
	}
}

func TestHealthErrorBudgetExhausted(t *testing.T) {
	clock := newStoreAt(time.Now())
	for range 4 {
		clock.store.Observe("a", &CallError{Class: ErrorRateLimit}, 0)
	}
	clock.store.Observe("a", nil, 1*time.Second) // 1 success, 4 errors
	if got := clock.store.health("a", 5*time.Minute, 0.2); got != 0 {
		t.Fatalf("over-budget provider health = %v, want 0", got)
	}
}

func TestHealthNeutralFailureDoesNotPoison(t *testing.T) {
	clock := newStoreAt(time.Now())
	// Configuration errors (model_not_found, invalid_response) are health-neutral:
	// they are recorded but must not raise the error rate.
	for range 3 {
		clock.store.Observe("a", &CallError{Class: ErrorModelNotFound}, 0)
	}
	clock.store.Observe("a", nil, 1*time.Second)
	if got := clock.store.health("a", 5*time.Minute, 0.2); got != 1 {
		t.Fatalf("neutral failures must not harm health, got %v", got)
	}
}

func TestHealthWindowExpiry(t *testing.T) {
	clock := newStoreAt(time.Now())
	clock.store.Observe("a", &CallError{Class: ErrorTimeout}, 0)
	clock.store.Observe("a", &CallError{Class: ErrorTimeout}, 0)
	clock.store.Observe("a", &CallError{Class: ErrorTimeout}, 0)
	clock.store.Observe("a", &CallError{Class: ErrorTimeout}, 0)
	clock.store.Observe("a", nil, 1*time.Second)
	if got := clock.store.health("a", 5*time.Minute, 0.2); got != 0 {
		t.Fatalf("before expiry: over-budget health = %v, want 0", got)
	}
	clock.advance(6 * time.Minute)
	if got := clock.store.health("a", 5*time.Minute, 0.2); got != 1 {
		t.Fatalf("after window expiry health = %v, want 1 (stale evidence pruned)", got)
	}
}

func TestLatencyFactorSkewsTowardFastest(t *testing.T) {
	clock := newStoreAt(time.Now())
	clock.store.Observe("a", nil, 1*time.Second)
	clock.store.Observe("b", nil, 3*time.Second)
	healthA := clock.store.health("a", 5*time.Minute, 0.2)
	healthB := clock.store.health("b", 5*time.Minute, 0.2)
	if !(healthA > healthB) {
		t.Fatalf("slower provider must get lower health: a=%v b=%v", healthA, healthB)
	}
}

func TestSelectRoundRobinRotatesOverHealthy(t *testing.T) {
	clock := newStoreAt(time.Now())
	// c is unhealthy (over budget); a and b are healthy.
	clock.store.Observe("c", &CallError{Class: ErrorUpstream}, 0)
	clock.store.Observe("c", &CallError{Class: ErrorUpstream}, 0)
	clock.store.Observe("c", &CallError{Class: ErrorUpstream}, 0)
	clock.store.Observe("c", nil, 1*time.Second) // 4 errors, 1 success
	policy := BalanceConfig{Enabled: true, Strategy: "round_robin", Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}, {Provider: "c"}}
	base := func(Target) int { return 1 }

	seenFirsts := map[string]bool{}
	for range 6 {
		out := clock.store.Select("r", targets, policy, base)
		seenFirsts[out[0].Provider] = true
		// The rotated candidate must always be healthy (never c while c is out).
		if out[0].Provider == "c" {
			t.Fatalf("unhealthy provider was balanced to the front")
		}
		if out[0].Provider == "a" && out[1].Provider != "b" {
			t.Fatalf("rotation must preserve order within the healthy set")
		}
	}
	if len(seenFirsts) != 2 || !seenFirsts["a"] || !seenFirsts["b"] {
		t.Fatalf("round_robin must rotate over healthy a,b only, got %#v", seenFirsts)
	}
}

func TestSelectAdaptiveFailOpenUnhealthyAll(t *testing.T) {
	clock := newStoreAt(time.Now())
	// Every pool provider is unhealthy (4 health errors, 1 success).
	for _, provider := range []string{"a", "b", "c"} {
		for range 4 {
			clock.store.Observe(provider, &CallError{Class: ErrorUpstream}, 0)
		}
		clock.store.Observe(provider, nil, 1*time.Second)
	}
	policy := BalanceConfig{Enabled: true, Strategy: "adaptive", Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}, {Provider: "c"}}
	// With every pool provider unhealthy, the store fails open to the base
	// order instead of refusing the request.
	out := clock.store.Select("r", targets, policy, func(Target) int { return 1 })
	if out[0].Provider != "a" {
		t.Fatalf("all-unhealthy selection must fail open to base order, got %#v", out)
	}
}

func TestSelectWeightedUsesStaticWeights(t *testing.T) {
	clock := newStoreAt(time.Now())
	policy := BalanceConfig{Enabled: true, Strategy: "weighted", Weights: map[string]int{"a": 9, "b": 1}, Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	counts := map[string]int{}
	for range 20 {
		out := clock.store.Select("r", targets, policy, func(Target) int { return 1 })
		counts[out[0].Provider]++
	}
	if counts["a"] == 0 || counts["b"] == 0 {
		t.Fatalf("weighted selection must pick both providers over repetitions, got %#v", counts)
	}
	if counts["a"] <= counts["b"] {
		t.Fatalf("9:1 weights must favour a, got %#v", counts)
	}
}

func TestHealthBudgetBoundary(t *testing.T) {
	clock := newStoreAt(time.Now())
	// 0.666 error rate vs a 0.6 budget → unhealthy.
	for range 2 {
		clock.store.Observe("a", &CallError{Class: ErrorRateLimit}, 0)
	}
	clock.store.Observe("a", nil, 1*time.Second)
	if got := clock.store.health("a", 5*time.Minute, 0.6); got != 0 {
		t.Fatalf("over-budget provider health = %v, want 0", got)
	}
	// 0.5 error rate vs a 0.666 budget → below budget; latency factor is 1
	// (single observed provider) so health = 1 - 0.5/0.666 ≈ 0.25.
	clock2 := newStoreAt(time.Now())
	clock2.store.Observe("a", &CallError{Class: ErrorRateLimit}, 0)
	clock2.store.Observe("a", nil, 1*time.Second)
	got := clock2.store.health("a", 5*time.Minute, 0.666)
	if math.Abs(got-0.25) > 0.01 {
		t.Fatalf("below-budget health = %v, want ~0.25", got)
	}
}

func TestScoreStoreConcurrentObserveAndSelect(t *testing.T) {
	clock := newStoreAt(time.Now())
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	policy := BalanceConfig{Enabled: true, Strategy: "adaptive", Window: 5 * time.Minute, ErrorBudget: 0.2}
	base := func(Target) int { return 1 }
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				clock.store.Observe("a", &CallError{Class: ErrorRateLimit, Status: 429}, 40*time.Millisecond)
				clock.store.Observe("b", nil, 20*time.Millisecond)
				clock.store.Select("r", targets, policy, base)
				clock.store.Healthy("a", 5*time.Minute, 0.2)
			}
		}()
	}
	wg.Wait()
}
