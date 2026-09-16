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

	seenFirsts := map[string]bool{}
	for range 6 {
		out := clock.store.Select("r", targets, policy)
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
	out := clock.store.Select("r", targets, policy)
	if out[0].Provider != "a" {
		t.Fatalf("all-unhealthy selection must fail open to base order, got %#v", out)
	}
}

func TestSelectWeightedUsesStaticWeights(t *testing.T) {
	// Weighted selection is random in production, so drive it deterministically
	// through the store's pick01 hook: a cycle of ten draws that lands nine
	// times in a's bin (<0.9) and once in b's bin (>=0.9) for 9:1 weights.
	// This exercises the exact 9:1 split instead of leaving the assertion to
	// chance (20 draws at 9:1 would historically skip b ~12% of the time).
	draws := []float64{0.05, 0.15, 0.25, 0.35, 0.45, 0.55, 0.65, 0.75, 0.85, 0.95}
	clock := newStoreAt(time.Now())
	var i int
	clock.store.pick01 = func() float64 {
		v := draws[i%len(draws)]
		i++
		return v
	}
	policy := BalanceConfig{Enabled: true, Strategy: "weighted", Weights: map[string]int{"a": 9, "b": 1}, Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	counts := map[string]int{}
	for range 20 {
		out := clock.store.Select("r", targets, policy)
		counts[out[0].Provider]++
	}
	if counts["a"] == 0 || counts["b"] == 0 {
		t.Fatalf("weighted selection must pick both providers over repetitions, got %#v", counts)
	}
	if counts["a"] <= counts["b"] {
		t.Fatalf("9:1 weights must favour a, got %#v", counts)
	}
	// Two full ten-draw cycles reproduce the 9:1 weighted split exactly.
	if counts["a"] != 18 || counts["b"] != 2 {
		t.Fatalf("9:1 weights must split 9:1 over a full cycle, got %#v", counts)
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
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				clock.store.Observe("a", &CallError{Class: ErrorRateLimit, Status: 429}, 40*time.Millisecond)
				clock.store.Observe("b", nil, 20*time.Millisecond)
				clock.store.Select("r", targets, policy)
				clock.store.Healthy("a", 5*time.Minute, 0.2)
			}
		}()
	}
	wg.Wait()
}

// TestInFlightTracking unit-checks the live per-provider in-flight counters
// behind the p2c strategy: increments and decrements balance out and a
// decrement never drops the count below zero.
func TestInFlightTracking(t *testing.T) {
	clock := newStoreAt(time.Now())
	if got := clock.store.inFlightOf("a"); got != 0 {
		t.Fatalf("fresh provider in-flight = %d, want 0", got)
	}
	clock.store.IncrInFlight("a")
	clock.store.IncrInFlight("a")
	if got := clock.store.inFlightOf("a"); got != 2 {
		t.Fatalf("after two launches in-flight = %d, want 2", got)
	}
	clock.store.DecrInFlight("a")
	if got := clock.store.inFlightOf("a"); got != 1 {
		t.Fatalf("after one completion in-flight = %d, want 1", got)
	}
	clock.store.DecrInFlight("a")
	clock.store.DecrInFlight("a") // stale decrement: floor at zero
	if got := clock.store.inFlightOf("a"); got != 0 {
		t.Fatalf("stale decrement must not drop below zero, got %d", got)
	}
}

func TestSelectP2CPrefersLowerInFlight(t *testing.T) {
	clock := newStoreAt(time.Now())
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	policy := BalanceConfig{Enabled: true, Strategy: "p2c", Window: 5 * time.Minute, ErrorBudget: 0.2}
	// b carries three live branches; a carries none.
	for range 3 {
		clock.store.IncrInFlight("b")
	}
	// Both draw orders must converge on the less-loaded provider: the second
	// draw maps over the remaining candidates, so (0, 0) draws (a, b) and
	// (0.99, 0) draws (b, a) — both orderings must promote a.
	for _, draws := range [][]float64{{0.0, 0.0}, {0.99, 0.0}} {
		var i int
		clock.store.pick01 = func() float64 {
			v := draws[i%len(draws)]
			i++
			return v
		}
		out := clock.store.Select("r", targets, policy)
		if out[0].Provider != "a" {
			t.Fatalf("p2c must promote the provider with fewer in-flight branches (draws %v), got %#v", draws, out)
		}
	}
}

func TestSelectP2CSkipsUnhealthy(t *testing.T) {
	clock := newStoreAt(time.Now())
	// c is over its error budget: it must never be drawn as a candidate.
	for range 4 {
		clock.store.Observe("c", &CallError{Class: ErrorUpstream}, 0)
	}
	clock.store.Observe("c", nil, 1*time.Second) // 4 errors, 1 success
	policy := BalanceConfig{Enabled: true, Strategy: "p2c", Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}, {Provider: "c"}}
	clock.store.pick01 = func() float64 { return 0.99 } // draws the last healthy candidate
	for range 10 {
		out := clock.store.Select("r", targets, policy)
		if out[0].Provider == "c" {
			t.Fatalf("unhealthy provider must never win the p2c choice, got %#v", out)
		}
	}
}

func TestSelectP2CSingleHealthyCandidate(t *testing.T) {
	clock := newStoreAt(time.Now())
	// b and c are over budget: a is the only healthy candidate.
	for _, provider := range []string{"b", "c"} {
		for range 4 {
			clock.store.Observe(provider, &CallError{Class: ErrorUpstream}, 0)
		}
		clock.store.Observe(provider, nil, 1*time.Second)
	}
	policy := BalanceConfig{Enabled: true, Strategy: "p2c", Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}, {Provider: "c"}}
	for range 5 {
		out := clock.store.Select("r", targets, policy)
		if out[0].Provider != "a" {
			t.Fatalf("single healthy candidate must always win, got %#v", out)
		}
	}
}

func TestSelectP2CWeightsBiasFirstDraw(t *testing.T) {
	// Explicit weights bias the first p2c draw, as documented. Draw 0.6 picks
	// b under equal weights (b's bin starts at 0.5 of total 2) but a under
	// 3:1 weights (a's bin spans [0, 0.75) of total 4); both providers are
	// idle, so the in-flight tie keeps the first draw.
	clock := newStoreAt(time.Now())
	clock.store.pick01 = func() float64 { return 0.6 }
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	equal := BalanceConfig{Enabled: true, Strategy: "p2c", Window: 5 * time.Minute, ErrorBudget: 0.2}
	weighted := equal
	weighted.Weights = map[string]int{"a": 3, "b": 1}
	if out := clock.store.Select("r", targets, equal); out[0].Provider != "b" {
		t.Fatalf("equal weights must leave the first draw uniform (0.6 draws b), got %#v", out)
	}
	if got := clock.store.Select("r", targets, weighted); got[0].Provider != "a" {
		t.Fatalf("explicit 3:1 weights must bias the first draw to a, got %#v", got)
	}
}

func TestSelectP2CDistributesIdlePool(t *testing.T) {
	// With every provider idle (in-flight 0) the p2c choice is the random
	// first draw: over a deterministic cycle both providers must win.
	draws := []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.95}
	clock := newStoreAt(time.Now())
	var i int
	clock.store.pick01 = func() float64 {
		v := draws[i%len(draws)]
		i++
		return v
	}
	policy := BalanceConfig{Enabled: true, Strategy: "p2c", Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	counts := map[string]int{}
	for range 20 {
		out := clock.store.Select("r", targets, policy)
		counts[out[0].Provider]++
	}
	if counts["a"] == 0 || counts["b"] == 0 {
		t.Fatalf("idle pool must distribute uniformly, got %#v", counts)
	}
}

func TestSelectAdaptiveDefaultWeightsAreEqual(t *testing.T) {
	// Without explicit weights every provider carries weight 1, regardless of
	// its provider priority (f7-13: priority-weighted selection concentrates).
	// Drive the selection deterministically: an alternating pick01 cycle must
	// split the traffic evenly across a full cycle.
	draws := []float64{0.25, 0.75}
	clock := newStoreAt(time.Now())
	var i int
	clock.store.pick01 = func() float64 {
		v := draws[i%len(draws)]
		i++
		return v
	}
	policy := BalanceConfig{Enabled: true, Strategy: "adaptive", Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	counts := map[string]int{}
	for range 20 {
		out := clock.store.Select("r", targets, policy)
		counts[out[0].Provider]++
	}
	if counts["a"] != 10 || counts["b"] != 10 {
		t.Fatalf("equal default weights must split evenly over a full cycle, got %#v", counts)
	}
}

func TestSelectExplicitWeightsStillWin(t *testing.T) {
	// The ten-draw cycle spans total weight 4 (3+1): draws < 0.75 land in a's
	// bin [0,3), >= 0.75 in b's [3,4) — i.e. 7:3 per cycle, 14:6 over two
	// cycles. The point is that explicit weights beat the equal default.
	draws := []float64{0.05, 0.15, 0.25, 0.35, 0.45, 0.55, 0.65, 0.75, 0.85, 0.95}
	clock := newStoreAt(time.Now())
	var i int
	clock.store.pick01 = func() float64 {
		v := draws[i%len(draws)]
		i++
		return v
	}
	policy := BalanceConfig{Enabled: true, Strategy: "adaptive", Weights: map[string]int{"a": 3, "b": 1}, Window: 5 * time.Minute, ErrorBudget: 0.2}
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	counts := map[string]int{}
	for range 20 {
		out := clock.store.Select("r", targets, policy)
		counts[out[0].Provider]++
	}
	if counts["a"] != 14 || counts["b"] != 6 {
		t.Fatalf("explicit 3:1 weights must beat the equal default (7:3 per draw cycle), got %#v", counts)
	}
}

func TestScoreStoreInFlightConcurrentSelect(t *testing.T) {
	// The p2c signal must be race-free against concurrent Incr/Decr and Select.
	clock := newStoreAt(time.Now())
	targets := []Target{{Provider: "a"}, {Provider: "b"}}
	policy := BalanceConfig{Enabled: true, Strategy: "p2c", Window: 5 * time.Minute, ErrorBudget: 0.2}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				clock.store.IncrInFlight("a")
				clock.store.Select("r", targets, policy)
				clock.store.DecrInFlight("a")
				clock.store.DecrInFlight("b")
			}
		}()
	}
	wg.Wait()
	if got := clock.store.inFlightOf("a"); got != 0 {
		t.Fatalf("in-flight must return to zero after balanced inc/dec, got %d", got)
	}
}
