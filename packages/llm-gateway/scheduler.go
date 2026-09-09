package main

import (
	"context"
	"errors"
	"time"
)

// branchResult is the terminal outcome of one upstream call produced by a
// branch goroutine. It is delivered exactly once through the schedule results
// channel.
type branchResult struct {
	id       int
	provider string
	winner   bool
	body     []byte
	selected *SelectedStream
	started  time.Time
	finished time.Time
	err      *CallError
}

// routeOutcome carries the winner and its payload out of the scheduler.
// pinned marks a route narrowed by a known affinity mapping so the caller can
// keep the request fail-closed (no fallback, no cross-provider state replay).
type routeOutcome struct {
	provider string
	body     []byte
	selected *SelectedStream
	err      *CallError
	pinned   bool
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

// routeRuntime owns request-wide state shared by the primary and fallback
// routes. The deadline is absolute so backoff and every fallback stage consume
// the same timeout budget; semaphore counters are monotonic for the request.
type routeRuntime struct {
	deadline time.Time
	sem      semaphore
}

func newRouteRuntime(plan Plan) *routeRuntime {
	runtime := &routeRuntime{}
	if plan.RouteTimeout > 0 {
		runtime.deadline = time.Now().Add(plan.RouteTimeout)
	}
	runtime.sem = semaphore{
		maxCalls:            plan.Semaphore.MaxCalls,
		maxInFlight:         plan.Semaphore.MaxInFlight,
		maxCallsPerProvider: plan.Semaphore.MaxCallsPerProvider,
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

type failureAggregate struct {
	count        int
	allRetryable bool
	selectedID   int
	selected     *CallError
}

func (f *failureAggregate) add(id int, callErr *CallError, retryOn map[ErrorClass]bool) {
	if callErr == nil {
		callErr = &CallError{Class: ErrorInvalid, Status: 502}
	}
	retryable := retryOn[callErr.Class]
	if f.count == 0 {
		f.allRetryable = retryable
	} else {
		f.allRetryable = f.allRetryable && retryable
	}
	f.count++
	if f.selected == nil || preferFailure(callErr, retryable, id, f.selected, retryOn[f.selected.Class], f.selectedID) {
		f.selected = callErr
		f.selectedID = id
	}
}

func (f *failureAggregate) reset() {
	*f = failureAggregate{}
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

// planBatches slices the ranked pool into the initial race batch and the
// retry batches. scope="same" repeats the original selection; scope="next"
// takes the next unused ranked targets and never repeats a used provider.
// Pool exhaustion truncates the schedule.
func planBatches(plan Plan, ordered []Target) [][]Target {
	raceN := plan.RaceCount
	if raceN <= 0 || raceN > len(ordered) {
		raceN = len(ordered)
	}
	batches := [][]Target{append([]Target(nil), ordered[:raceN]...)}
	used := make(map[string]bool, len(ordered))
	for _, target := range ordered[:raceN] {
		used[target.Provider] = true
	}
	for i := 0; i < plan.Retry.Attempts; i++ {
		if plan.Retry.Scope == "same" {
			batches = append(batches, append([]Target(nil), ordered[:raceN]...))
			continue
		}
		count := plan.Retry.Count
		if count <= 0 {
			count = 1
		}
		batch := make([]Target, 0, count)
		for _, target := range ordered {
			if used[target.Provider] {
				continue
			}
			batch = append(batch, target)
			used[target.Provider] = true
			count--
			if count == 0 {
				break
			}
		}
		if len(batch) == 0 {
			break
		}
		batches = append(batches, batch)
	}
	return batches
}

// runSchedule executes the bounded route for both streaming and non-streaming
// paths with identical invariants:
//   - the initial race launches only the top batch;
//   - hedge launches only the next retry batch, never the full pool;
//   - a retryable completion of the whole active set continues the route
//     before the hedge timer after backoff;
//   - no new upstream calls start after a winner, cancellation, timeout, or
//     semaphore exhaustion;
//   - the schedule never hangs on pool exhaustion, partial permits, or a slow
//     cancelled executor call.
func (r *Runner) runSchedule(ctx context.Context, logical string, plan Plan, batches [][]Target, request ExecuteRequest, streamMode, pinned bool, runtime *routeRuntime) *routeOutcome {
	if len(batches) == 0 || len(batches[0]) == 0 {
		return &routeOutcome{err: &CallError{Class: ErrorInvalid, Status: 502, Cause: errors.New("empty route pool")}}
	}
	if pinned {
		batches = batches[:1]
	}

	sc := &schedule{
		r:        r,
		ctx:      ctx,
		logical:  logical,
		plan:     plan,
		batches:  batches,
		request:  request,
		stream:   streamMode,
		cancels:  make(map[int]context.CancelFunc),
		launched: make([]bool, len(batches)),
		sem:      &runtime.sem,
		runtime:  runtime,
	}
	sc.startedTargets = make([][]bool, len(batches))
	for i, batch := range batches {
		sc.startedTargets[i] = make([]bool, len(batch))
	}
	// The absolute request-wide deadline is observed by every schedule without
	// attaching it to the winning stream's context, so a stream selected before
	// the deadline remains usable after selection.
	deadlineCh, stopDeadline := runtime.deadlineTimer()
	defer stopDeadline()

	// Buffer the results channel so a cancelled branch never blocks the loop:
	// total slot count (or the semaphore cap) bounds the number of messages.
	capacity := 0
	for _, batch := range batches {
		capacity += len(batch)
	}
	sc.results = make(chan *branchResult, capacity)

	if !sc.launch(0) {
		sc.cancelAll()
		if runtime.expired() {
			return &routeOutcome{err: routeTimeoutError()}
		}
		return &routeOutcome{err: &CallError{Class: ErrorInvalid, Status: 502, Cause: errors.New("no upstream branch could start")}}
	}
	// Keep the cursor on a partially permitted initial batch. Otherwise
	// race.count > max_in_flight would silently discard the denied targets.
	if sc.complete(0) {
		sc.next = 1
	}
	sc.armHedge()

	var failures failureAggregate
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
				r.observeLeaseFailure(logical, plan, res.provider, res.err)
			}
			if res.winner {
				r.observeLeaseWinner(logical, plan, res.provider, res)
				sc.cancelOthers(res.id)
				outcome := &routeOutcome{provider: res.provider}
				if streamMode {
					outcome.selected = res.selected
				} else {
					outcome.body = res.body
				}
				return outcome
			}
			sc.active--
			failures.add(res.id, res.err, plan.Retry.On)
			// Refill a semaphore-limited initial race as soon as a slot is
			// released. These are members of the original batch, not retries,
			// so their launch is independent of the retry error filter/backoff.
			if sc.next == 0 {
				if sc.launchRemaining(sc.active == 0) {
					continue
				}
				if sc.active > 0 {
					continue
				}
			}
			if sc.active == 0 {
				aggregateErr := failures.err()
				if sc.next < len(sc.batches) && failures.allRetryable {
					if callErr := runtime.waitBackoff(ctx, backoffDuration(plan.Retry.Backoff, sc.next-1), r.sleep); callErr != nil {
						sc.cancelAll()
						return &routeOutcome{err: callErr}
					}
					if !sc.launchRemaining(true) {
						sc.cancelAll()
						if runtime.expired() {
							return &routeOutcome{err: routeTimeoutError()}
						}
						return &routeOutcome{err: aggregateErr}
					}
					failures.reset()
				} else {
					sc.cancelAll()
					return &routeOutcome{err: aggregateErr}
				}
			}
		case <-sc.hedgeC:
			sc.hedgeC = nil
			if runtime.expired() {
				sc.cancelAll()
				return &routeOutcome{err: routeTimeoutError()}
			}
			if sc.next < len(sc.batches) {
				sc.launchRemaining(false)
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

// schedule carries the per-request scheduler state. It is owned by the single
// scheduler goroutine except for branch goroutines sending on results.
type schedule struct {
	r              *Runner
	ctx            context.Context
	logical        string
	plan           Plan
	batches        [][]Target
	request        ExecuteRequest
	stream         bool
	results        chan *branchResult
	cancels        map[int]context.CancelFunc
	sem            *semaphore
	runtime        *routeRuntime
	launched       []bool
	startedTargets [][]bool
	next           int
	active         int
	seq            int
	hedgeC         <-chan time.Time
	hedgeTimer     *time.Timer
}

// launch starts the not-yet-started members of batch idx that still have a
// semaphore permit. Members denied a permit stay pending: the batch is marked
// exhausted only when every member has started, so a later hedge (or a retry
// wave after a slot frees) can start the denied members instead of dropping
// them silently.
func (sc *schedule) launch(idx int) bool {
	if sc.runtime.expired() || idx < 0 || idx >= len(sc.batches) || sc.launched[idx] {
		return false
	}
	started := false
	allStarted := true
	for i, target := range sc.batches[idx] {
		if sc.startedTargets[idx][i] {
			continue
		}
		if !sc.sem.acquire(target.Provider, sc.active) {
			allStarted = false
			continue
		}
		sc.startedTargets[idx][i] = true
		started = true
		sc.active++
		sc.seq++
		id := sc.seq
		branchCtx, cancel := context.WithCancel(sc.ctx)
		sc.cancels[id] = cancel
		go sc.runBranch(branchCtx, id, cancel, target)
	}
	if allStarted {
		sc.launched[idx] = true
	}
	return started
}

// complete reports whether every member of batch idx has started, i.e. no
// permit-denied member is still waiting for a later launch attempt.
func (sc *schedule) complete(idx int) bool {
	for _, started := range sc.startedTargets[idx] {
		if !started {
			return false
		}
	}
	return true
}

// launchRemaining attempts to launch the next unlaunched batch. When
// advanceOnZilch is true (no active branches, so permits are free and only
// total budget exhaustion can block) batches that cannot start a branch are
// skipped permanently; otherwise a blocked batch is kept for a later hedge.
// Returns whether any branch started.
func (sc *schedule) launchRemaining(advanceOnZilch bool) bool {
	for sc.next < len(sc.batches) {
		if sc.launch(sc.next) {
			// Keep the cursor on a partially started batch so its denied members
			// can still be launched by a later hedge or a retry wave.
			if sc.complete(sc.next) {
				sc.next++
			}
			sc.armHedge()
			return true
		}
		if advanceOnZilch {
			sc.launched[sc.next] = true
			sc.next++
			continue
		}
		sc.armHedge()
		return false
	}
	sc.armHedge()
	return false
}

func (sc *schedule) armHedge() {
	if sc.hedgeTimer != nil {
		sc.hedgeTimer.Stop()
		sc.hedgeTimer = nil
	}
	sc.hedgeC = nil
	if sc.plan.HedgeAfter > 0 && sc.next < len(sc.batches) {
		sc.hedgeTimer = time.NewTimer(sc.plan.HedgeAfter)
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

// runFallback executes the legacy terminal fallback route after the primary
// route failed with a matching class: the fallback pool is dispatched serially
// or as one parallel batch.
func (r *Runner) runFallback(ctx context.Context, logical string, fb FallbackRoute, request ExecuteRequest, streamMode bool, runtime *routeRuntime) *routeOutcome {
	if runtime.expired() {
		return &routeOutcome{err: routeTimeoutError()}
	}
	ids, err := expandAccessGroups(fb.Groups, r.config.providers, r.config.mappings[logical])
	if err != nil {
		return &routeOutcome{err: &CallError{Class: ErrorInvalid, Status: 502, Cause: err}}
	}
	available := r.availableProviders(ids)
	if len(available) == 0 {
		available = append([]string(nil), ids...)
	}
	targets := make([]Target, 0, len(available))
	for _, id := range available {
		target, targetErr := r.target(logical, id)
		if targetErr == nil {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return &routeOutcome{err: &CallError{Class: ErrorInvalid, Status: 502}}
	}
	if fb.Mode == "serial" {
		var last *CallError
		started := false
		for _, target := range targets {
			if runtime.expired() {
				return &routeOutcome{err: routeTimeoutError()}
			}
			if !runtime.sem.acquire(target.Provider, 0) {
				continue
			}
			started = true
			branchCtx, cancel := context.WithCancel(ctx)
			results := make(chan *branchResult, 1)
			go func() {
				if streamMode {
					results <- r.probeStream(branchCtx, target, request)
					return
				}
				body, callErr := r.executor.Do(branchCtx, target, request)
				callErr = normalizeContextError(branchCtx, callErr)
				results <- &branchResult{winner: callErr == nil, body: body, err: callErr}
			}()

			deadline, stopDeadline := runtime.deadlineTimer()
			var res *branchResult
			select {
			case res = <-results:
				stopDeadline()
			case <-deadline:
				stopDeadline()
				cancel()
				return &routeOutcome{err: routeTimeoutError()}
			case <-ctx.Done():
				stopDeadline()
				cancel()
				return &routeOutcome{err: streamSelectionContextError(ctx)}
			}
			if res.winner {
				r.record(target.Provider, nil)
				if streamMode {
					res.selected.Provider = target.Provider
					res.selected.Cancel = cancel
					return &routeOutcome{provider: target.Provider, selected: res.selected}
				}
				cancel()
				return &routeOutcome{provider: target.Provider, body: res.body}
			}
			cancel()
			if res.err == nil || res.err.Class != ErrorCancelled {
				r.record(target.Provider, res.err)
			}
			last = res.err
		}
		if !started {
			last = &CallError{Class: ErrorInvalid, Status: 502, Cause: errors.New("fallback semaphore budget exhausted")}
		} else if last == nil {
			last = &CallError{Class: ErrorInvalid, Status: 502}
		}
		return &routeOutcome{err: last}
	}
	// race mode (hedge mode is treated as one parallel batch for legacy routes).
	fallbackPlan := Plan{
		LogicalModel: logical,
		Pool:         ids,
		RaceCount:    len(targets),
	}
	return r.runSchedule(ctx, logical, fallbackPlan, [][]Target{targets}, request, streamMode, false, runtime)
}
