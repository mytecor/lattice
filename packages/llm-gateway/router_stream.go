package main

import (
	"context"
	"errors"
	"time"
)

// SelectedStream is the winner of a streaming route: the buffered prelude, the
// remaining event stream, the cancellation handle, and the winning provider.
type SelectedStream struct {
	Buffered  []StreamEvent
	Remaining <-chan StreamEvent
	Cancel    context.CancelFunc
	Provider  string
	// Model is the winner's native model (empty when the caller does not
	// carry one, e.g. a continuation takeover built outside the scheduler).
	Model string
	// TTFT is the winner's time to first meaningful event.
	TTFT time.Duration
	// Attempts is the number of route executions dispatched for the request.
	Attempts int
	Release  func()
}

// SelectStream executes the bounded streaming route and returns the winner.
// The winner is chosen only by the first meaningful content, reasoning, or
// tool-call event; losers are cancelled and never affect health or lease state.
func (r *Runner) SelectStream(ctx context.Context, logical string, request ExecuteRequest) (*SelectedStream, *CallError) {
	entry, ok := r.config.models[logical]
	if !ok {
		return nil, &CallError{Class: ErrorInvalid, Status: 404, Scope: FailureScopeRequest}
	}
	if request.StreamIdleTimeout <= 0 {
		request.StreamIdleTimeout = r.streamIdleTimeout(logical)
	}
	runtime := newRouteRuntime(entry)
	release, admitErr := r.admit(ctx, logical, runtime)
	if admitErr != nil {
		return nil, admitErr
	}
	outcome := r.executeRoute(ctx, logical, entry, request, true, runtime)
	if outcome.err != nil {
		release()
		return nil, outcome.err
	}
	outcome.selected.TTFT = outcome.ttft
	outcome.selected.Attempts = outcome.attempts
	outcome.selected.Release = release
	return outcome.selected, nil
}

// ContinueStream re-dispatches a streaming chat request to a different
// provider after the previously chosen winner stream stalled or broke before
// a finish_reason (the "continue" routing rule). It appends the already
// relayed partial output as an assistant context message (reshare) so the
// next provider continues the answer instead of starting from scratch, and it
// excludes the providers that actually broke during this request (via the
// route runtime's exclude set) so the continuation does not re-hit the same
// broken upstream. Providers that merely lost the original race stay eligible:
// they were cancelled at the winner's first meaningful event, not broken.
// It returns the fresh selected stream, or the terminal failure when no
// remaining provider can serve the continuation; the caller then decides
// whether to surface the original stream's structured error.
func (r *Runner) ContinueStream(ctx context.Context, logical string, request ExecuteRequest, partial *partialStreamOutput, broken []string) (*SelectedStream, *CallError) {
	entry, ok := r.config.models[logical]
	if !ok {
		return nil, &CallError{Class: ErrorInvalid, Status: 404, Cause: errors.New("continue: unknown logical model")}
	}
	if request.Kind != RequestChat {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: errors.New("continue requires a chat completion request")}
	}
	rewritten, err := appendPartialChatHistory(request.Body, partial)
	if err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 502, Cause: err}
	}
	request.Body = rewritten
	if request.StreamIdleTimeout <= 0 {
		request.StreamIdleTimeout = r.streamIdleTimeout(logical)
	}
	runtime := newRouteRuntime(entry)
	// A continuation must never re-race a provider that is still cooling
	// (it just broke): the strict policy disables the availability fail-open,
	// so an all-cooling pool yields no targets here instead of re-adding the
	// whole cooling pool that fail-open would otherwise bring back. Fresh
	// selection keeps fail-open; a continuation deliberately does not.
	runtime.strictAvailability = true
	for _, provider := range broken {
		if provider != "" {
			runtime.exclude[provider] = true
		}
	}
	outcome := r.executeRoute(ctx, logical, entry, request, true, runtime)
	if outcome.err != nil {
		return nil, outcome.err
	}
	if outcome.selected == nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 502, Cause: errors.New("continue selected no stream")}
	}
	outcome.selected.TTFT = outcome.ttft
	outcome.selected.Attempts = outcome.attempts
	return outcome.selected, nil
}

// streamIdleTimeout returns the per-chunk idle timeout for a logical model's
// relayed winner stream: explicit continue.idle wins, then the route's timeout
// rule (idle or duration), and finally the config fallback.
func (r *Runner) streamIdleTimeout(logical string) time.Duration {
	if entry, ok := r.config.models[logical]; ok {
		if entry.Continue.Enabled && entry.Continue.Idle > 0 {
			return entry.Continue.Idle
		}
		if entry.StreamIdleTimeout > 0 {
			return entry.StreamIdleTimeout
		}
		if entry.RouteTimeout > 0 {
			return entry.RouteTimeout
		}
	}
	return r.config.raw.StreamIdleTimeout.Duration
}

// continuePolicy reports the compiled in-gateway takeover policy (the
// "continue" rule) of a logical model's entry route, and whether it is active.
func (r *Runner) continuePolicy(logical string) (ContinueConfig, bool) {
	entry, ok := r.config.models[logical]
	if !ok {
		return ContinueConfig{}, false
	}
	if entry.Continue.Enabled {
		return entry.Continue, true
	}
	if entry.Retry.Internal && entry.Retry.Attempts > 0 {
		idle := r.streamIdleTimeout(logical)
		if idle <= 0 {
			idle = 30 * time.Second
		}
		wait := entry.StreamIdleTimeout
		if wait <= 0 {
			wait = entry.RouteTimeout
		}
		if wait <= 0 {
			wait = continueDefaultWait
		}
		return ContinueConfig{
			Enabled: true,
			Idle:    idle,
			Reshare: "full",
			Retries: entry.Retry.Attempts,
			Wait:    wait,
		}, true
	}
	return ContinueConfig{}, false
}

// repetitionPolicy reports the compiled in-gateway loop-guard policy (the
// "repetition" rule) of a logical model's entry route, and whether it is
// active. Absence means no loop detection: the relay behaves exactly as
// before, mirroring how an absent continue rule disables takeovers.
func (r *Runner) repetitionPolicy(logical string) (RepetitionConfig, bool) {
	entry, ok := r.config.models[logical]
	if !ok {
		return RepetitionConfig{}, false
	}
	return entry.Repetition, entry.Repetition.Enabled
}
