package main

import (
	"context"
	"errors"
	"time"
)

// branchResult is the terminal outcome of one upstream call produced by a
// branch goroutine (or a locally catalog-rejected target reported without an
// upstream call, see deliverPreFailed). It is delivered exactly once through
// the schedule results channel.
type branchResult struct {
	id        int
	provider  string
	winner    bool
	prefailed bool // catalog-rejected before dispatch: no upstream call, no semaphore slot
	body      []byte
	selected  *SelectedStream
	started   time.Time
	finished  time.Time
	err       *CallError
}

// routeOutcome carries the winner and its payload out of the scheduler.
// pinned marks a route narrowed by a known affinity mapping so the caller can
// keep the request fail-closed (no transitions). empty marks a transition
// route whose dynamic pool had no available targets: the transition is not
// applicable and must never mask the original terminal failure.
type routeOutcome struct {
	provider string
	body     []byte
	selected *SelectedStream
	err      *CallError
	pinned   bool
	empty    bool
}

// emptyPoolError is the distinguishable terminal result of a transition route
// whose dynamic pool has no available targets for the current request (for
// example every provider is already used by this request graph).
func emptyPoolError() *CallError {
	return &CallError{Class: ErrorInvalid, Status: 502, Cause: errors.New("route has no available targets")}
}

// semaphore enforces the per-request safety bounds. It is owned by the single
// scheduler goroutine, so it needs no internal synchronization.
type semaphore struct {
	maxCalls            int
	maxInFlight         int
	maxCallsPerProvider int
	calls               int
	perProvider         map[string]int
}

// routeRuntime owns request-wide state shared by the whole route graph. The
// deadline is absolute so backoff and every transition consume the same
// timeout budget; semaphore counters and the used-provider set are monotonic
// for the request and never reset when entering a subroute.
type routeRuntime struct {
	deadline time.Time
	sem      semaphore
	used     map[string]bool
}

func newRouteRuntime(entry *compiledRoute) *routeRuntime {
	runtime := &routeRuntime{used: make(map[string]bool)}
	if entry != nil && entry.RouteTimeout > 0 {
		runtime.deadline = time.Now().Add(entry.RouteTimeout)
	}
	runtime.sem = semaphore{
		maxCalls:            entry.Semaphore.MaxCalls,
		maxInFlight:         entry.Semaphore.MaxInFlight,
		maxCallsPerProvider: entry.Semaphore.MaxCallsPerProvider,
		perProvider:         make(map[string]int),
	}
	return runtime
}

func (r *routeRuntime) expired() bool {
	return !r.deadline.IsZero() && !time.Now().Before(r.deadline)
}

func (r *routeRuntime) deadlineTimer() (<-chan time.Time, func()) {
	if r.deadline.IsZero() {
		return nil, func() {}
	}
	duration := time.Until(r.deadline)
	if duration < 0 {
		duration = 0
	}
	timer := time.NewTimer(duration)
	return timer.C, func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

func routeTimeoutError() *CallError {
	return &CallError{Class: ErrorTimeout, Status: 504, Cause: context.DeadlineExceeded}
}

// waitBackoff sleeps without letting a retry backoff hide the route deadline.
// The injected sleeper keeps scheduler timing tests deterministic.
func (r *routeRuntime) waitBackoff(ctx context.Context, duration time.Duration, sleep func(context.Context, time.Duration) error) *CallError {
	if r.expired() {
		return routeTimeoutError()
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sleep(waitCtx, duration) }()
	deadline, stopDeadline := r.deadlineTimer()
	defer stopDeadline()
	select {
	case err := <-done:
		if err == nil {
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) || r.expired() {
			return routeTimeoutError()
		}
		return &CallError{Class: ErrorCancelled, Status: 499, Cause: err}
	case <-deadline:
		return routeTimeoutError()
	case <-ctx.Done():
		return streamSelectionContextError(ctx)
	}
}

// failureAggregate collects the terminal failures of one route execution and
// selects the externally observed error deterministically, independent of the
// completion order of the branch goroutines. A non-retryable failure dominates
// a retryable one; ties use a stable class priority and finally launch order.
type failureAggregate struct {
	count      int
	selectedID int
	selected   *CallError
}

func (f *failureAggregate) add(id int, callErr *CallError, retryable func(ErrorClass) bool) {
	if callErr == nil {
		callErr = &CallError{Class: ErrorInvalid, Status: 502}
	}
	candidateRetryable := retryable != nil && retryable(callErr.Class)
	if f.selected == nil || preferFailure(callErr, candidateRetryable, id, f.selected, retryable != nil && retryable(f.selected.Class), f.selectedID) {
		f.selected = callErr
		f.selectedID = id
	}
	f.count++
}

func (f *failureAggregate) err() *CallError {
	if f.selected != nil {
		return f.selected
	}
	return &CallError{Class: ErrorInvalid, Status: 502}
}

// preferFailure makes the externally observed error independent of goroutine
// completion order. A non-retryable failure dominates a retryable one; ties
// use a stable class priority and finally branch launch order.
func preferFailure(candidate *CallError, candidateRetryable bool, candidateID int, current *CallError, currentRetryable bool, currentID int) bool {
	if candidateRetryable != currentRetryable {
		return !candidateRetryable
	}
	candidatePriority := failurePriority(candidate.Class)
	currentPriority := failurePriority(current.Class)
	if candidatePriority != currentPriority {
		return candidatePriority > currentPriority
	}
	return candidateID < currentID
}

func failurePriority(class ErrorClass) int {
	switch class {
	case ErrorInvalid:
		return 7
	case ErrorNotFound:
		return 6
	case ErrorRateLimit:
		return 5
	case ErrorUpstream:
		return 4
	case ErrorConnection:
		return 3
	case ErrorTimeout:
		return 2
	case ErrorCancelled:
		return 1
	default:
		return 0
	}
}

func (s *semaphore) acquire(provider string, inFlight int) bool {
	if s.maxCalls > 0 && s.calls >= s.maxCalls {
		return false
	}
	if s.maxInFlight > 0 && inFlight >= s.maxInFlight {
		return false
	}
	if s.maxCallsPerProvider > 0 && s.perProvider[provider] >= s.maxCallsPerProvider {
		return false
	}
	s.calls++
	s.perProvider[provider]++
	return true
}

// schedule owns the per-request branch launcher of one route execution: the
// route's race batch (group 0) plus, when configured and armed, the hedge
// target batch (group 1). It is owned by the single scheduler goroutine except
// for branch goroutines sending on results.
type schedule struct {
	r       *Runner
	ctx     context.Context
	logical string
	route   *compiledRoute
	groups  [][]Target
	// armed[g] says whether group g may launch: the race group is armed from
	// the start, the hedge group only after its delay elapses.
	armed []bool
	// excludeUsed[g] skips members whose provider is already used by the
	// request graph (the hedge target's unused routing policy, applied at
	// launch time because the used set grows while the source races).
	excludeUsed []bool
	// hedgeG limits how many hedge members may actually launch: the unused
	// policy is applied at launch time, so the hedge group carries the full
	// ordered pool and the race count caps the launches.
	limits     []int
	started    [][]bool
	launched   []bool
	request    ExecuteRequest
	stream     bool
	results    chan *branchResult
	cancels    map[int]context.CancelFunc
	sem        *semaphore
	runtime    *routeRuntime
	active     int
	seq        int
	hedgeG     int
	hedgeC     <-chan time.Time
	hedgeTimer *time.Timer
}

// launch starts the not-yet-started members of group g that still have a
// semaphore permit. Members denied a permit stay pending: the group is marked
// exhausted only when every member has started, so a later slot release (or
// the hedge) can start the denied members instead of dropping them silently.
// Locally catalog-rejected members are reported as pre-failed results without
// an upstream call or a semaphore slot. Returns whether any branch started or
// was reported pre-failed.
func (sc *schedule) launch(g int) bool {
	if sc.runtime.expired() || g < 0 || g >= len(sc.groups) || !sc.armed[g] || sc.launched[g] {
		return false
	}
	started := false
	delivered := false
	allStarted := true
	launchedN := 0
	limit := len(sc.groups[g])
	if sc.limits[g] > 0 && sc.limits[g] < limit {
		limit = sc.limits[g]
	}
	for i, target := range sc.groups[g] {
		if sc.started[g][i] {
			continue
		}
		if sc.excludeUsed[g] && sc.runtime.used[target.Provider] {
			// The member's provider is already used by the request graph: the
			// unused routing policy of this group permanently skips it.
			sc.started[g][i] = true
			continue
		}
		if launchedN >= limit {
			// Beyond the race-count limit of this group: consume the member so
			// the group can be exhausted. The amount actually launched is
			// bounded by the race count even when earlier members were skipped
			// by the unused policy.
			sc.started[g][i] = true
			continue
		}
		// Exact catalog validation happens before any semaphore slot is
		// consumed: a locally rejected target (model_not_found in the
		// snapshot, or an explicit catalog without a snapshot) is not an
		// upstream call, so the call budgets stay available for a different
		// native alias of the same provider in a later route.
		if callErr := sc.r.validateTarget(target); callErr != nil {
			sc.started[g][i] = true
			delivered = true
			sc.seq++
			sc.deliverPreFailed(sc.seq, target, callErr)
			launchedN++
			continue
		}
		if !sc.sem.acquire(target.Provider, sc.active) {
			allStarted = false
			continue
		}
		sc.started[g][i] = true
		started = true
		launchedN++
		sc.active++
		sc.seq++
		id := sc.seq
		sc.runtime.used[target.Provider] = true
		branchCtx, cancel := context.WithCancel(sc.ctx)
		sc.cancels[id] = cancel
		go sc.runBranch(branchCtx, id, cancel, target)
	}
	if allStarted {
		sc.launched[g] = true
	}
	return started || delivered
}

// deliverPreFailed reports a catalog-rejected target as a terminal branch
// result without an upstream call or a semaphore slot. Pre-failed results do
// not mark the provider as used, so a provider with a different native alias
// can still be reached in a later route.
func (sc *schedule) deliverPreFailed(id int, target Target, callErr *CallError) {
	started := sc.r.now()
	res := &branchResult{
		id: id, provider: target.Provider, winner: false, prefailed: true,
		err: callErr, started: started, finished: started,
	}
	select {
	case sc.results <- res:
	case <-sc.ctx.Done():
	}
}

// refill attempts to launch the still-pending members of every armed,
// not-yet-exhausted group (a semaphore slot was just released). Returns
// whether any branch started.
func (sc *schedule) refill() bool {
	started := false
	for g := range sc.groups {
		if !sc.armed[g] || sc.launched[g] {
			continue
		}
		if sc.launch(g) {
			started = true
		}
	}
	return started
}

func (sc *schedule) armHedge() {
	if sc.hedgeG < 0 {
		return
	}
	if sc.hedgeTimer != nil {
		sc.hedgeTimer.Stop()
		sc.hedgeTimer = nil
	}
	sc.hedgeC = nil
	if !sc.armed[sc.hedgeG] && !sc.launched[sc.hedgeG] && sc.route.Hedge.After > 0 {
		sc.hedgeTimer = time.NewTimer(sc.route.Hedge.After)
		sc.hedgeC = sc.hedgeTimer.C
	}
}

func (sc *schedule) cancelOthers(except int) {
	for id, cancel := range sc.cancels {
		if id != except {
			cancel()
		}
	}
}

func (sc *schedule) cancelAll() {
	for _, cancel := range sc.cancels {
		cancel()
	}
}

// runBranch performs one upstream call and delivers exactly one terminal
// result. The send never blocks the caller past cancellation.
func (sc *schedule) runBranch(ctx context.Context, id int, cancel context.CancelFunc, target Target) {
	started := sc.r.now()
	var res *branchResult
	if sc.stream {
		res = sc.r.probeStream(ctx, target, sc.request)
		if res.selected != nil {
			res.selected.Provider = target.Provider
			res.selected.Cancel = cancel
		}
	} else {
		body, callErr := sc.r.executor.Do(ctx, target, sc.request)
		callErr = normalizeContextError(ctx, callErr)
		res = &branchResult{winner: callErr == nil, body: body, err: callErr}
	}
	res.id = id
	res.provider = target.Provider
	res.started = started
	if res.finished.IsZero() {
		res.finished = sc.r.now()
	}
	select {
	case sc.results <- res:
	case <-ctx.Done():
	}
}

// drain buffered terminal results into the aggregate. At the terminal point no
// branch is in flight, so everything still buffered was reported as pre-failed
// (catalog-rejected) and never consumed a slot.
func (sc *schedule) drainPrefailed(failures *failureAggregate, retryable func(ErrorClass) bool) {
	for {
		select {
		case res := <-sc.results:
			if !res.winner && res.err == nil {
				res.err = &CallError{Class: ErrorInvalid, Status: 502}
			}
			failures.add(res.id, res.err, retryable)
		default:
			return
		}
	}
}

// raceRoute executes one compiled route: its race batch (group 0) plus an
// optional latency hedge batch (group 1) that starts only while branches are
// still in flight. It returns the winner or the deterministic terminal failure
// of the route execution. No retry/fallback scheduling lives here: those are
// explicit transitions owned by the caller (see executeRoute).
func (r *Runner) raceRoute(ctx context.Context, logical string, route *compiledRoute, request ExecuteRequest, streamMode bool, runtime *routeRuntime) *routeOutcome {
	pool, pinned, callErr := r.buildPool(logical, route, request, runtime)
	if callErr != nil {
		return &routeOutcome{err: callErr, pinned: pinned}
	}
	if len(pool) == 0 {
		return &routeOutcome{err: emptyPoolError(), empty: true}
	}
	ordered := r.applyLease(logical, route, pool)
	raceN := route.RaceCount
	if raceN <= 0 || raceN > len(ordered) {
		raceN = len(ordered)
	}
	raceBatch := append([]Target(nil), ordered[:raceN]...)
	if len(raceBatch) == 0 {
		return &routeOutcome{err: &CallError{Class: ErrorInvalid, Status: 502, Cause: errors.New("empty route pool")}}
	}

	groups := [][]Target{raceBatch}
	hedgeG := -1
	if route.hedgeTarget != nil && !pinned {
		hedgeBatch := r.buildHedgeBatch(logical, route.hedgeTarget, request, runtime)
		if len(hedgeBatch) > 0 {
			hedgeG = len(groups)
			groups = append(groups, hedgeBatch)
		}
	}

	totalTargets := 0
	for _, group := range groups {
		totalTargets += len(group)
	}
	sc := &schedule{
		r:       r,
		ctx:     ctx,
		logical: logical,
		route:   route,
		groups:  groups,
		request: request,
		stream:  streamMode,
		results: make(chan *branchResult, totalTargets),
		cancels: make(map[int]context.CancelFunc),
		sem:     &runtime.sem,
		runtime: runtime,
		hedgeG:  hedgeG,
	}
	sc.armed = make([]bool, len(groups))
	sc.excludeUsed = make([]bool, len(groups))
	sc.limits = make([]int, len(groups))
	sc.started = make([][]bool, len(groups))
	sc.launched = make([]bool, len(groups))
	for g := range groups {
		sc.started[g] = make([]bool, len(groups[g]))
		sc.limits[g] = len(groups[g])
	}
	sc.armed[0] = true
	if hedgeG >= 0 && route.hedgeTarget != nil {
		// The hedge target's unused routing policy is enforced at launch time,
		// because the request's used set grows while the source route races;
		// the race count caps how many hedge members may actually launch.
		sc.excludeUsed[hedgeG] = route.hedgeTarget.ProviderUnused
		if route.hedgeTarget.RaceCount > 0 {
			sc.limits[hedgeG] = route.hedgeTarget.RaceCount
		}
	}

	// The absolute request-wide deadline is observed by every schedule without
	// attaching it to the winning stream's context, so a stream selected before
	// the deadline remains usable after selection.
	deadlineCh, stopDeadline := runtime.deadlineTimer()
	defer stopDeadline()

	if !sc.launch(0) {
		sc.cancelAll()
		if runtime.expired() {
			return &routeOutcome{err: routeTimeoutError()}
		}
		// Every member is blocked by the request-wide semaphore budget: the
		// route has no available targets for this request.
		return &routeOutcome{err: emptyPoolError(), empty: true}
	}
	sc.armHedge()

	// A failure admits a retry transition when it matches the retry target's
	// own error filter; the flag only orders the deterministic selection below.
	retryable := func(ErrorClass) bool { return false }
	if route.retryTarget != nil {
		retryable = func(class ErrorClass) bool { return route.retryTarget.ErrorIn[class] }
	}

	failures := &failureAggregate{}
	for {
		select {
		case res := <-sc.results:
			if !res.winner && res.err == nil {
				res.err = &CallError{Class: ErrorInvalid, Status: 502}
			}
			// Cancellations (route timeout, client cancel, loser cancel) are
			// neutral for health and lease state.
			if res.err == nil || res.err.Class != ErrorCancelled {
				r.record(res.provider, res.err)
				r.observeLeaseFailure(logical, route, res.provider, res.err)
			}
			if res.winner {
				r.observeLeaseWinner(logical, route, res.provider, res)
				sc.cancelOthers(res.id)
				outcome := &routeOutcome{provider: res.provider}
				if streamMode {
					outcome.selected = res.selected
				} else {
					outcome.body = res.body
				}
				return outcome
			}
			if !res.prefailed {
				sc.active--
			}
			failures.add(res.id, res.err, retryable)
			// A slot just freed: try to start members of an armed group that were
			// denied a permit while it was held (refills the race batch and the
			// hedge batch alike).
			sc.refill()
			if sc.active == 0 {
				// No branch is in flight and the refill started nothing: no
				// future event can free another slot, so the route is terminal.
				// An as-yet-unarmed hedge is abandoned by design (hedge is
				// latency-only and must not delay a fast terminal failure).
				sc.drainPrefailed(failures, retryable)
				sc.cancelAll()
				return &routeOutcome{err: failures.err(), pinned: pinned}
			}
		case <-sc.hedgeC:
			sc.hedgeC = nil
			if runtime.expired() {
				sc.cancelAll()
				return &routeOutcome{err: routeTimeoutError()}
			}
			if sc.hedgeG >= 0 {
				sc.armed[sc.hedgeG] = true
				sc.launch(sc.hedgeG)
			}
		case <-deadlineCh:
			sc.cancelAll()
			return &routeOutcome{err: routeTimeoutError()}
		case <-ctx.Done():
			sc.cancelAll()
			return &routeOutcome{err: streamSelectionContextError(ctx)}
		}
	}
}

// collectFailures is unused: prefailed results are accumulated through the
// main loop and drained at the terminal point.
