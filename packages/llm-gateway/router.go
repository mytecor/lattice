package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type ErrorClass string

const (
	ErrorTimeout       ErrorClass = "timeout"
	ErrorConnection    ErrorClass = "connection_error"
	ErrorRateLimit     ErrorClass = "429"
	ErrorNotFound      ErrorClass = "404"
	ErrorModelNotFound ErrorClass = "model_not_found"
	ErrorUpstream      ErrorClass = "5xx"
	ErrorInvalid       ErrorClass = "invalid_response"
	ErrorCancelled     ErrorClass = "cancelled"
)

type CallError struct {
	Class  ErrorClass
	Status int
	Cause  error
}

func (e *CallError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Class)
}

type Target struct {
	Provider string
	Model    string
}

type RequestKind string

const (
	RequestChat      RequestKind = "chat"
	RequestResponses RequestKind = "responses"
)

type ExecuteRequest struct {
	Kind RequestKind
	Body []byte
}

type StreamEvent struct {
	Data       []byte
	Event      string
	Meaningful bool
	Done       bool
	Err        *CallError
}

type Executor interface {
	Do(context.Context, Target, ExecuteRequest) ([]byte, *CallError)
	Stream(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError)
	Close() error
}

// RunOutcome carries the winner of a non-streaming request.
type RunOutcome struct {
	Body     []byte
	Provider string
}

type Runner struct {
	config   *compiledConfig
	catalog  *Catalog
	executor Executor
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	mu       sync.Mutex
	cooling  map[string]time.Time
	leases   *LeaseStore
	affinity *AffinityStore
}

func newRunner(config *compiledConfig, catalog *Catalog, executor Executor) *Runner {
	runner := &Runner{
		config: config, catalog: catalog, executor: executor,
		now: time.Now, sleep: sleepContext, cooling: make(map[string]time.Time),
	}
	runner.leases = newLeaseStore(runner.now)
	runner.affinity = newAffinityStore(runner.now, config.raw.AffinityFile, func(err error) {
		config.logger.Error("affinity persistence failed", "detail", safeLogDetail(err.Error()))
	})
	return runner
}

// Close shuts down the runner's persistent state: the affinity store flushes
// any recent mappings so a graceful restart does not lose them.
func (r *Runner) Close() error {
	if r.affinity != nil {
		return r.affinity.Close()
	}
	return nil
}

// Run executes the bounded non-streaming route graph and returns the winner
// body.
func (r *Runner) Run(ctx context.Context, logical string, request ExecuteRequest) ([]byte, *CallError) {
	outcome := r.runPlan(ctx, logical, request, false)
	if outcome.err != nil {
		return nil, outcome.err
	}
	return outcome.body, nil
}

// RunWithResult executes the non-streaming route graph and also reports the
// winning provider, needed for lease and affinity bookkeeping.
func (r *Runner) RunWithResult(ctx context.Context, logical string, request ExecuteRequest) (*RunOutcome, *CallError) {
	outcome := r.runPlan(ctx, logical, request, false)
	if outcome.err != nil {
		return nil, outcome.err
	}
	return &RunOutcome{Body: outcome.body, Provider: outcome.provider}, nil
}

// runPlan resolves the request-level entry route for the logical model and
// executes the compiled route graph with one request-wide runtime: the
// deadline, the semaphore budget and the used-provider set are shared by every
// transition and never reset when entering a subroute.
func (r *Runner) runPlan(ctx context.Context, logical string, request ExecuteRequest, streamMode bool) *routeOutcome {
	entry, ok := r.config.models[logical]
	if !ok {
		return &routeOutcome{err: &CallError{Class: ErrorInvalid, Status: 404}}
	}
	runtime := newRouteRuntime(entry)
	return r.executeRoute(ctx, logical, entry, request, streamMode, runtime)
}

// executeRoute runs one compiled route and applies its explicit transitions on
// a terminal failure: retry is a bounded repeated transition into the retry
// target (with backoff), fallback a one-shot alternate transition. The
// destination owns its applicability through its own filter; a non-applicable
// destination returns the current terminal failure unchanged, and a destination
// with no available targets (empty) never masks the original failure. A
// cancellation or a known affinity pin (fail-closed) terminates the graph.
func (r *Runner) executeRoute(ctx context.Context, logical string, route *compiledRoute, request ExecuteRequest, streamMode bool, runtime *routeRuntime) *routeOutcome {
	outcome := r.raceRoute(ctx, logical, route, request, streamMode, runtime)
	if outcome.err == nil || outcome.err.Class == ErrorCancelled || outcome.pinned {
		return outcome
	}
	if outcome.empty {
		return outcome
	}
	original := outcome.err

	if route.Retry.Attempts > 0 && route.retryTarget != nil {
		for attempt := 0; attempt < route.Retry.Attempts; attempt++ {
			if !route.retryTarget.applicable(outcome.err, attempt) {
				break
			}
			if callErr := runtime.waitBackoff(ctx, backoffDuration(route.Retry.Backoff, attempt), r.sleep); callErr != nil {
				return &routeOutcome{err: callErr}
			}
			next := r.executeRoute(ctx, logical, route.retryTarget, request, streamMode, runtime)
			if next.err == nil || next.err.Class == ErrorCancelled {
				return next
			}
			if next.empty {
				// The retry route has no available targets for this request:
				// the transition is not usable, so restore the original failure
				// and continue to any sibling fallback transition.
				outcome = &routeOutcome{err: original}
				break
			}
			outcome = next
		}
	}
	if route.fallbackTarget != nil && route.fallbackTarget.applicable(outcome.err, 0) {
		next := r.executeRoute(ctx, logical, route.fallbackTarget, request, streamMode, runtime)
		if !next.empty {
			return next
		}
	}
	return outcome
}

// buildPool produces the runtime candidate pool of a route for the request. A
// known affinity mapping narrows the route to the pinned provider for the
// whole request graph; an unknown state identifier is ignored (on_missing =
// "ignore"). The unused-provider routing policy excludes providers already
// used by this request graph; cooling providers are skipped with fail-open so
// the route is never artificially idle. Exact catalog validation happens at
// branch execution, so a missing native produces a model_not_found failure
// that can activate the configured fallback.
func (r *Runner) buildPool(logical string, route *compiledRoute, request ExecuteRequest, runtime *routeRuntime) ([]Target, bool, *CallError) {
	if route.Affinity.Enabled && request.Kind == RequestResponses {
		if id := requestAffinityID(request.Body, request.Kind, route.Affinity.Sources); id != "" {
			if provider, ok := r.affinity.Lookup(id); ok {
				if _, exists := r.config.providers[provider]; !exists {
					// A mapping can outlive a configuration change that removes
					// or renames its provider. Only that structural case is
					// treated as missing affinity; runtime resolution failures
					// stay fail-closed.
					r.affinity.Forget(id)
				} else {
					for _, target := range route.Pool {
						if target.Provider == provider {
							return []Target{target}, true, nil
						}
					}
					// The pinned provider is not a member of the compiled pool:
					// treat the mapping as stale and forget it (same structural
					// boundary).
					r.affinity.Forget(id)
				}
			}
		}
	}
	pool, callErr := r.buildDynamicPool(route, request, runtime)
	if callErr != nil {
		return nil, false, callErr
	}
	return pool, false, nil
}

// buildDynamicPool applies the route's own runtime selection: the
// unused-provider policy (explicit routing policy, not a hidden retry
// property) and the cooling fail-open. An all-used pool returns empty with no
// error, so the caller can report the route as not applicable.
func (r *Runner) buildDynamicPool(route *compiledRoute, _ ExecuteRequest, runtime *routeRuntime) ([]Target, *CallError) {
	pool := route.Pool
	if route.ProviderUnused {
		filtered := make([]Target, 0, len(pool))
		for _, target := range pool {
			if !runtime.used[target.Provider] {
				filtered = append(filtered, target)
			}
		}
		if len(filtered) == 0 {
			return nil, nil
		}
		pool = filtered
	}
	return r.availableFailOpen(pool), nil
}

// availableFailOpen skips cooling providers and fails open with the full pool
// when every target is cooling, so the route is never artificially idle.
func (r *Runner) availableFailOpen(pool []Target) []Target {
	available := r.availableTargets(pool)
	if len(available) == 0 {
		return append([]Target(nil), pool...)
	}
	return available
}

// buildHedgeBatch derives the hedge target route's runtime pool (the full
// ordered selection, not yet capped): the hedge target's unused routing policy
// and race count are applied at launch time, because the request's used set
// grows while the source route races. Here only cooling (with fail-open) and
// the lease promotion are applied.
func (r *Runner) buildHedgeBatch(logical string, target *compiledRoute, _ ExecuteRequest, runtime *routeRuntime) []Target {
	pool := r.availableFailOpen(target.Pool)
	if len(pool) == 0 {
		return nil
	}
	return r.applyLease(logical, target, pool)
}

// applyLease promotes the current lease holder to the top of the ranking.
func (r *Runner) applyLease(logical string, route *compiledRoute, pool []Target) []Target {
	if !route.Lease.Enabled {
		return pool
	}
	holder, ok := r.leases.Holder(logical)
	if !ok {
		return pool
	}
	for i, target := range pool {
		if target.Provider == holder && i > 0 {
			reordered := make([]Target, 0, len(pool))
			reordered = append(reordered, target)
			reordered = append(reordered, pool[:i]...)
			reordered = append(reordered, pool[i+1:]...)
			return reordered
		}
	}
	return pool
}

// validateTarget checks the explicit native model of a target against the
// provider's last-known-good catalog snapshot before dispatch:
//   - snapshot available, native present: valid;
//   - snapshot available, native absent: model_not_found (may activate fallback);
//   - implicit catalog not yet available: optimistic call of the configured ID;
//   - explicit catalog unavailable without a snapshot: fail closed.
func (r *Runner) validateTarget(target Target) *CallError {
	return r.catalog.Validate(target.Provider, target.Model)
}

func (r *Runner) availableTargets(targets []Target) []Target {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	available := make([]Target, 0, len(targets))
	for _, target := range targets {
		if until, ok := r.cooling[target.Provider]; !ok || !now.Before(until) {
			available = append(available, target)
		}
	}
	return available
}

func (r *Runner) record(providerID string, callErr *CallError) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if callErr == nil {
		delete(r.cooling, providerID)
		return
	}
	if allRetryableClasses()[callErr.Class] {
		r.cooling[providerID] = r.now().Add(r.config.providers[providerID].Cooldown.Duration)
	}
}

// observeLeaseFailure releases the model lease when its holder fails with a
// configured hard-failure class. Cancelled losers never reach this method.
func (r *Runner) observeLeaseFailure(logical string, route *compiledRoute, provider string, callErr *CallError) {
	if !route.Lease.Enabled || callErr == nil || callErr.Class == ErrorCancelled {
		return
	}
	if route.Lease.ReleaseOn[callErr.Class] {
		r.leases.ReleaseIfHolder(logical, provider)
	}
}

// observeLeaseWinner renews or acquires the lease for the winner and accounts
// its start-to-meaningful time for the consecutive slow-start counter.
func (r *Runner) observeLeaseWinner(logical string, route *compiledRoute, provider string, res *branchResult) {
	if !route.Lease.Enabled {
		return
	}
	if route.Lease.RenewOnSuccess || !r.leases.Exists(logical) {
		r.leases.Renew(logical, provider, route.Lease.Duration)
	}
	if route.Lease.ReleaseAfterSlowStarts > 0 && route.Lease.SlowStart > 0 {
		if res.finished.Sub(res.started) > route.Lease.SlowStart {
			r.leases.ObserveSlowStart(logical, provider, route.Lease.ReleaseAfterSlowStarts)
		} else {
			r.leases.ResetSlowStarts(logical, provider)
		}
	}
}

// affinityBindTTL returns the affinity TTL for a logical model when affinity is
// enabled, so the server can persist response id mappings.
func (r *Runner) affinityBindTTL(logical string) (time.Duration, bool) {
	entry, ok := r.config.models[logical]
	if !ok || !entry.Affinity.Enabled {
		return 0, false
	}
	return entry.Affinity.TTL, true
}

func backoffDuration(config BackoffConfig, retryIndex int) time.Duration {
	initial := config.Initial.Duration
	if initial <= 0 {
		initial = 100 * time.Millisecond
	}
	maximum := config.Max.Duration
	if maximum <= 0 {
		maximum = time.Second
	}
	if strings.ToLower(config.Type) != "exponential" {
		return min(initial, maximum)
	}
	delay := initial
	for range retryIndex {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	return min(delay, maximum)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizeContextError(ctx context.Context, callErr *CallError) *CallError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &CallError{Class: ErrorTimeout, Status: 504, Cause: ctx.Err()}
	}
	if errors.Is(ctx.Err(), context.Canceled) && callErr != nil {
		return &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
	}
	return callErr
}

func streamSelectionContextError(ctx context.Context) *CallError {
	if ctx.Err() == context.DeadlineExceeded {
		return &CallError{Class: ErrorTimeout, Status: 504, Cause: context.DeadlineExceeded}
	}
	return &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
}

// probeStream reads one streaming branch until the first meaningful content,
// reasoning or tool-call event, buffering the prelude. Cancellations of losers
// are neutral for health and lease state.
func (r *Runner) probeStream(ctx context.Context, target Target, request ExecuteRequest) *branchResult {
	stream, callErr := r.executor.Stream(ctx, target, request)
	res := &branchResult{}
	if callErr != nil {
		res.err = callErr
		return res
	}
	buffered := make([]StreamEvent, 0, 4)
	for {
		select {
		case <-ctx.Done():
			class := ErrorCancelled
			status := 499
			if ctx.Err() == context.DeadlineExceeded {
				class = ErrorTimeout
				status = 504
			}
			res.err = &CallError{Class: class, Status: status, Cause: ctx.Err()}
			return res
		case event, ok := <-stream:
			if !ok {
				res.err = &CallError{Class: ErrorInvalid, Status: 502}
				return res
			}
			if event.Err != nil {
				res.err = normalizeContextError(ctx, event.Err)
				return res
			}
			buffered = append(buffered, event)
			if event.Meaningful {
				res.winner = true
				res.finished = r.now()
				res.selected = &SelectedStream{Buffered: buffered, Remaining: stream}
				return res
			}
		}
	}
}
