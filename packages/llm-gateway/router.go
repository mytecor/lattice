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
	ErrorTimeout    ErrorClass = "timeout"
	ErrorConnection ErrorClass = "connection_error"
	ErrorRateLimit  ErrorClass = "429"
	ErrorNotFound   ErrorClass = "404"
	ErrorUpstream   ErrorClass = "5xx"
	ErrorInvalid    ErrorClass = "invalid_response"
	ErrorCancelled  ErrorClass = "cancelled"
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

// Run executes the bounded non-streaming route and returns the winner body.
func (r *Runner) Run(ctx context.Context, logical string, request ExecuteRequest) ([]byte, *CallError) {
	outcome := r.runPlan(ctx, logical, request, false)
	if outcome.err != nil {
		return nil, outcome.err
	}
	return outcome.body, nil
}

// RunWithResult executes the non-streaming route and also reports the winning
// provider, needed for lease and affinity bookkeeping.
func (r *Runner) RunWithResult(ctx context.Context, logical string, request ExecuteRequest) (*RunOutcome, *CallError) {
	outcome := r.runPlan(ctx, logical, request, false)
	if outcome.err != nil {
		return nil, outcome.err
	}
	return &RunOutcome{Body: outcome.body, Provider: outcome.provider}, nil
}

func (r *Runner) runPlan(ctx context.Context, logical string, request ExecuteRequest, streamMode bool) *routeOutcome {
	plan, ok := r.config.plans[logical]
	if !ok {
		return &routeOutcome{err: &CallError{Class: ErrorInvalid, Status: 404}}
	}
	runtime := newRouteRuntime(plan)
	outcome := r.runPrimary(ctx, logical, plan, request, streamMode, runtime)
	// A known affinity mapping narrows the route to one provider and must fail
	// closed on its failure: a legacy fallback group would move the stateful
	// chain to another provider without a state replay, so it is suppressed.
	if !outcome.pinned && plan.Fallback != nil && outcome.err != nil && plan.Fallback.On[outcome.err.Class] {
		return r.runFallback(ctx, logical, *plan.Fallback, request, streamMode, runtime)
	}
	return outcome
}

func (r *Runner) runPrimary(ctx context.Context, logical string, plan Plan, request ExecuteRequest, streamMode bool, runtime *routeRuntime) *routeOutcome {
	pool, pinned, callErr := r.buildPool(logical, plan, request)
	if callErr != nil {
		return &routeOutcome{err: callErr, pinned: pinned}
	}
	ordered := r.applyLease(logical, plan, pool)
	batches := planBatches(plan, ordered)
	outcome := r.runSchedule(ctx, logical, plan, batches, request, streamMode, pinned, runtime)
	outcome.pinned = pinned
	return outcome
}

// buildPool produces the candidate target pool for the request. A known
// affinity mapping narrows the route to the pinned provider; an unknown state
// identifier is ignored (on_missing = "ignore") and results in the normal
// pool. Cooling and unresolvable providers are skipped; an all-cooling pool
// fail-opens so the route is never artificially idle.
func (r *Runner) buildPool(logical string, plan Plan, request ExecuteRequest) ([]Target, bool, *CallError) {
	if plan.Affinity.Enabled && request.Kind == RequestResponses {
		if id := requestAffinityID(request.Body, request.Kind, plan.Affinity.Sources); id != "" {
			if provider, ok := r.affinity.Lookup(id); ok {
				if _, exists := r.config.providers[provider]; !exists {
					// A mapping can outlive a configuration change that removes or
					// renames its provider. Only that structural case is treated as
					// missing affinity; runtime resolution failures stay fail-closed.
					r.affinity.Forget(id)
				} else {
					target, targetErr := r.target(logical, provider)
					if targetErr == nil {
						return []Target{target}, true, nil
					}
					return nil, true, targetErr
				}
			}
		}
	}
	ids := r.availableProviders(plan.Pool)
	if len(ids) == 0 {
		ids = append([]string(nil), plan.Pool...)
	}
	pool := make([]Target, 0, len(ids))
	for _, id := range ids {
		target, targetErr := r.target(logical, id)
		if targetErr == nil {
			pool = append(pool, target)
		}
	}
	if len(pool) == 0 {
		return nil, false, &CallError{Class: ErrorInvalid, Status: 502, Cause: errors.New("provider pool is empty")}
	}
	return pool, false, nil
}

// applyLease promotes the current lease holder to the top of the ranking.
func (r *Runner) applyLease(logical string, plan Plan, pool []Target) []Target {
	if !plan.Lease.Enabled {
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

func (r *Runner) target(logical, providerID string) (Target, *CallError) {
	provider, ok := r.config.providers[providerID]
	if !ok {
		return Target{}, &CallError{Class: ErrorInvalid, Status: 502}
	}
	primary := r.config.mappings[logical][provider.Name]
	model, err := r.catalog.Resolve(provider.Name, primary)
	if err != nil {
		return Target{}, &CallError{Class: ErrorInvalid, Status: 503, Cause: err}
	}
	return Target{Provider: providerID, Model: model}, nil
}

func (r *Runner) availableProviders(ids []string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	available := make([]string, 0, len(ids))
	for _, id := range ids {
		if until, ok := r.cooling[id]; !ok || !now.Before(until) {
			available = append(available, id)
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
func (r *Runner) observeLeaseFailure(logical string, plan Plan, provider string, callErr *CallError) {
	if !plan.Lease.Enabled || callErr == nil || callErr.Class == ErrorCancelled {
		return
	}
	if plan.Lease.ReleaseOn[callErr.Class] {
		r.leases.ReleaseIfHolder(logical, provider)
	}
}

// observeLeaseWinner renews or acquires the lease for the winner and accounts
// its start-to-meaningful time for the consecutive slow-start counter.
func (r *Runner) observeLeaseWinner(logical string, plan Plan, provider string, res *branchResult) {
	if !plan.Lease.Enabled {
		return
	}
	if plan.Lease.RenewOnSuccess || !r.leases.Exists(logical) {
		r.leases.Renew(logical, provider, plan.Lease.Duration)
	}
	if plan.Lease.ReleaseAfterSlowStarts > 0 && plan.Lease.SlowStart > 0 {
		if res.finished.Sub(res.started) > plan.Lease.SlowStart {
			r.leases.ObserveSlowStart(logical, provider, plan.Lease.ReleaseAfterSlowStarts)
		} else {
			r.leases.ResetSlowStarts(logical, provider)
		}
	}
}

// affinityBindTTL returns the affinity TTL for a logical model when affinity is
// enabled, so the server can persist response id mappings.
func (r *Runner) affinityBindTTL(logical string) (time.Duration, bool) {
	plan, ok := r.config.plans[logical]
	if !ok || !plan.Affinity.Enabled {
		return 0, false
	}
	return plan.Affinity.TTL, true
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
