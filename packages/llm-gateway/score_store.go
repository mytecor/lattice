package main

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// ScoreStore is the runtime provider-health state behind the balance action.
// It is the analogue of LeaseStore: memory-only, per-provider (globally
// across logical models) and fed from scheduler observations at the same
// points where cooldown and lease state are recorded. Health is a provider
// property, so the store is not scoped by logical model.
//
// For every completed upstream branch it tracks:
//   - an EWMA of the time to the first meaningful event (stream) or to the
//     successful response (non-stream), computed from winner latencies;
//   - a sliding window of results (success / health error), where a health
//     error is one of the balance-confirmed failure classes (429, 5xx,
//     timeout, connection_error).
//
// Health(p) ∈ [0,1] combines the windowed error rate relative to the route's
// error budget with a relative latency factor: a provider whose windowed
// error rate meets or exceeds the budget is unhealthy (health 0), and among
// healthy providers the one with the lowest EWMA latency sets the latency
// factor baseline. Cancellations are neutral (the scheduler never feeds
// them), exactly like lease and cooldown state.
type ScoreStore struct {
	mu        sync.Mutex
	now       func() time.Time
	pick01    func() float64 // injected for deterministic weighted-random tests
	providers map[string]*providerScore
	rrCursor  map[string]int // per-route round-robin cursor
	inFlight  map[string]int // live in-flight branch count per provider (p2c signal)
}

type providerScore struct {
	latency   float64 // EWMA of first-meaningful success latency, seconds
	latencyOK bool
	samples   []scoreSample // sliding window, appended on every relevant branch
}

type scoreSample struct {
	at        time.Time
	success   bool
	healthErr bool
}

func newScoreStore(now func() time.Time) *ScoreStore {
	if now == nil {
		now = time.Now
	}
	return &ScoreStore{
		now:       now,
		pick01:    rand.Float64,
		providers: make(map[string]*providerScore),
		rrCursor:  make(map[string]int),
		inFlight:  make(map[string]int),
	}
}

// IncrInFlight and DecrInFlight maintain the live in-flight branch count per
// provider — the load signal behind the p2c strategy. They are called by the
// scheduler at exactly the same points as the llm_requests_in_flight gauge
// (branch launch and branch completion), so the balance signal and the
// exported gauge always describe the same concurrency state. Scope: a
// streamed branch completes at winner selection (the first meaningful event),
// so the signal — and the gauge — cover the probe phase of a stream, not the
// whole relayed response. A decrement never drops below zero, so a stale
// increment cannot poison the signal.
func (s *ScoreStore) IncrInFlight(provider string) {
	if provider == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight[provider]++
}

func (s *ScoreStore) DecrInFlight(provider string) {
	if provider == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value := s.inFlight[provider]; value > 0 {
		s.inFlight[provider] = value - 1
	}
}

// inFlightOf reports the current in-flight branch count of one provider.
// Providers without any observed branch carry zero.
func (s *ScoreStore) inFlightOf(provider string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight[provider]
}

// balanceHealthErrors are the failure classes that count against a provider's
// health budget. Configuration errors (model_not_found, 404, invalid_response)
// and client cancellations are health-neutral: they say nothing about whether
// the provider is alive and fast.
func balanceHealthErrors(class ErrorClass) bool {
	switch class {
	case ErrorRateLimit, ErrorUpstream, ErrorTimeout, ErrorConnection:
		return true
	}
	return false
}

// Observe records one completed upstream branch for a provider. Latency is
// only meaningful for successful branches; failures append a health-error
// sample when their class counts against the budget, and a neutral failure
// otherwise. Cancelled branches never reach this method.
func (s *ScoreStore) Observe(provider string, callErr *CallError, latency time.Duration) {
	if provider == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	score := s.providers[provider]
	if score == nil {
		score = &providerScore{}
		s.providers[provider] = score
	}
	now := s.now()
	success := callErr == nil
	sample := scoreSample{at: now, success: success}
	if callErr != nil {
		sample.healthErr = balanceHealthErrors(callErr.Class)
	}
	score.samples = append(score.samples, sample)
	if success {
		// EWMA with α=0.3 (window ≈ 3 samples): smooth but responsive enough
		// that a provider recovering from a bad spell starts winning back
		// influence instead of being cemented out by a long stale history.
		const alpha = 0.3
		seconds := latency.Seconds()
		if !score.latencyOK {
			score.latency = seconds
			score.latencyOK = true
		} else {
			score.latency = alpha*seconds + (1-alpha)*score.latency
		}
	}
}

// health computes [0,1] for one provider using the route's balance policy.
// An empty window keeps every provider nominally healthy (no error rate, no
// latency baseline), so the very first requests fail open to the base order.
// It takes the store lock because concurrent requests may Observe in parallel.
func (s *ScoreStore) health(provider string, window time.Duration, errorBudget float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	score, ok := s.providers[provider]
	if !ok {
		return 1
	}
	now := s.now()
	cutoff := now.Add(-window)
	// The window is pruned in place from the front; samples are appended in
	// time order so the window is contiguous.
	for len(score.samples) > 0 && !score.samples[0].at.After(cutoff) {
		score.samples = score.samples[1:]
	}
	total := 0
	errors := 0
	for _, sample := range score.samples {
		if sample.healthErr {
			errors++
		}
		if sample.healthErr || sample.success {
			total++
		}
	}
	if total == 0 {
		return 1
	}
	errorRate := float64(errors) / float64(total)
	if errorRate >= errorBudget {
		return 0
	}
	errorFactor := 1 - errorRate/errorBudget
	// Relative latency factor: the provider with the best (lowest) EWMA
	// latency across the store sets the baseline. Without a latency baseline
	// for any provider the factor stays 1.
	minLatency := math.Inf(1)
	haveBaseline := false
	for _, other := range s.providers {
		if other.latencyOK {
			minLatency = math.Min(minLatency, other.latency)
			haveBaseline = true
		}
	}
	latencyFactor := 1.0
	if haveBaseline && score.latencyOK && minLatency > 0 && score.latency > 0 {
		latencyFactor = minLatency / score.latency
		if latencyFactor > 1 {
			latencyFactor = 1
		}
	}
	// The latency factor only skews among providers that are already inside
	// their error budget; it can never zero a healthy provider.
	return errorFactor * latencyFactor
}

// Health reports the numeric [0,1] health score of a provider for the given
// balance policy window and budget. It is the public form of the internal
// health computation, used both by the balance selection and by the
// observability snapshot.
func (s *ScoreStore) Health(provider string, window time.Duration, errorBudget float64) float64 {
	return s.health(provider, window, errorBudget)
}

// Healthy reports whether the provider is inside its error budget for the
// route's balance policy.
func (s *ScoreStore) Healthy(provider string, window time.Duration, errorBudget float64) bool {
	return s.health(provider, window, errorBudget) > 0
}

// baseWeight resolves the static per-provider weight of a target for the
// balance policy: an explicit policy weight wins; without one every provider
// carries the same weight (1). Provider priority is deliberately not a base
// for runtime weights — f7-13 showed priority-weighted selection concentrates
// on the highest-priority provider; priority only shapes compile-time pool
// order.
func baseWeight(target Target, policy BalanceConfig) int {
	if policyW, ok := policy.Weights[target.Provider]; ok && policyW > 0 {
		return policyW
	}
	return 1
}

// healthyIndexes returns the pool indexes whose provider is inside the health
// floor for the balance policy. p2c, round_robin and adaptive skip providers
// whose windowed error rate meets or exceeds the error budget; weighted is
// static and keeps every provider so manual tuning stays authoritative.
func (s *ScoreStore) healthyIndexes(targets []Target, policy BalanceConfig) []int {
	indexes := make([]int, 0, len(targets))
	for i := range targets {
		switch policy.Strategy {
		case "weighted":
			indexes = append(indexes, i)
		default:
			if s.health(targets[i].Provider, policy.Window, policy.ErrorBudget) > 0 {
				indexes = append(indexes, i)
			}
		}
	}
	return indexes
}

// Select chooses and promotes one provider for the balance action: it moves
// the chosen target to the front so a `race count = 1` executes exactly that
// target deterministically (and a `race count > 1` still races the chosen
// provider first). Candidates weaker than the health floor are skipped with
// fail-open: if every candidate is unhealthy the whole pool is used with base
// order only, so the route is never artificially idle.
//
// Strategies:
//
//	p2c          — power of two choices: two random healthy candidates are
//	  drawn and the one with fewer in-flight branches wins. Spreads load under
//	  concurrency without latency feedback (f7-13 showed latency-weighted
//	  selection re-concentrates on the fastest provider).
//	round_robin — a per-route cursor rotates over the healthy candidates,
//	  excluding the unhealthy via the health floor. Maximum distribution.
//	adaptive    — weighted-random by score(p) = base(p) × health(p): spreads
//	  the load and shifts it toward who is currently coping best.
//	weighted    — only the static weights, no health history.
func (s *ScoreStore) Select(route string, targets []Target, policy BalanceConfig) []Target {
	if len(targets) < 2 {
		return targets
	}
	healthy := s.healthyIndexes(targets, policy)
	if len(healthy) == 0 {
		// Fail-open: no healthy candidate at all → base order unchanged so
		// the route keeps working instead of refusing the request.
		return targets
	}
	switch policy.Strategy {
	case "p2c":
		return s.selectP2C(targets, policy, healthy)
	case "round_robin":
		return s.selectRoundRobin(route, targets, healthy)
	case "adaptive", "weighted":
		weights := make([]float64, len(healthy))
		for i, idx := range healthy {
			w := float64(baseWeight(targets[idx], policy))
			if policy.Strategy == "adaptive" {
				w *= s.health(targets[idx].Provider, policy.Window, policy.ErrorBudget)
			}
			weights[i] = w
		}
		chosen := s.pickWeighted(healthy, weights)
		return promoteFront(targets, targets[healthy[chosen]].Provider)
	default:
		return targets
	}
}

// selectP2C implements the power-of-two-choices strategy: it draws two
// distinct random candidates from the healthy set and promotes the one with
// fewer in-flight branches. Under concurrency this equalizes the queues
// without any latency feedback, which f7-13 identified as the missing
// distribution signal. Ties in in-flight keep the first draw, so the result
// is deterministic under the injected pick01 while the draws themselves stay
// random: an idle pool distributes uniformly. The first draw is weighted by
// the static base weight (equal by default); the second is uniform over the
// remaining candidates.
func (s *ScoreStore) selectP2C(targets []Target, policy BalanceConfig, healthy []int) []Target {
	if len(healthy) == 1 {
		return promoteFront(targets, targets[healthy[0]].Provider)
	}
	// The first draw is weighted by the static base weight (equal 1 by
	// default, so the idle-pool draw is uniform; explicit weights bias it).
	weights := make([]float64, len(healthy))
	for i, idx := range healthy {
		weights[i] = float64(baseWeight(targets[idx], policy))
	}
	first := s.pickWeighted(healthy, weights)
	if first < 0 {
		first = 0
	}
	second := int(s.pick01() * float64(len(healthy)-1))
	if second >= first {
		second++
	}
	a, b := healthy[first], healthy[second]
	if s.inFlightOf(targets[a].Provider) <= s.inFlightOf(targets[b].Provider) {
		return promoteFront(targets, targets[a].Provider)
	}
	return promoteFront(targets, targets[b].Provider)
}

// selectRoundRobin rotates a per-route cursor over the healthy candidates in
// pool order, so consecutive requests land on different providers. The cursor
// is stored in the score store keyed by route; it is not a lease and grants
// no favour to the selected provider beyond this one request.
func (s *ScoreStore) selectRoundRobin(route string, targets []Target, healthy []int) []Target {
	s.mu.Lock()
	cursor := s.rrCursor[route]
	s.mu.Unlock()
	cursor %= len(healthy)
	next := healthy[cursor]
	s.mu.Lock()
	s.rrCursor[route] = (cursor + 1) % len(healthy)
	s.mu.Unlock()
	return promoteFront(targets, targets[next].Provider)
}

// pickWeighted selects one index by weighted random over the given weight
// pairs (indexes and weights). It returns -1 when the total weight is not
// positive.
func (s *ScoreStore) pickWeighted(indexes []int, weights []float64) int {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return -1
	}
	target := s.pick01() * total
	acc := 0.0
	for i, w := range weights {
		acc += w
		if target < acc {
			return i
		}
	}
	return len(indexes) - 1
}

// promoteFront returns a copy of targets with the first target of the given
// provider moved to the front (the relative order of the rest is preserved).
func promoteFront(targets []Target, provider string) []Target {
	for i, target := range targets {
		if target.Provider == provider {
			if i == 0 {
				return targets
			}
			reordered := make([]Target, 0, len(targets))
			reordered = append(reordered, target)
			reordered = append(reordered, targets[:i]...)
			reordered = append(reordered, targets[i+1:]...)
			return reordered
		}
	}
	return targets
}
