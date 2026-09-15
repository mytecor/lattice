package main

import (
	"context"
	"time"
)

// SelectedStream is the winner of a streaming route: the buffered prelude, the
// remaining event stream, the cancellation handle, and the winning provider.
type SelectedStream struct {
	Buffered  []StreamEvent
	Remaining <-chan StreamEvent
	Cancel    context.CancelFunc
	Provider  string
	// TTFT is the winner's time to first meaningful event.
	TTFT time.Duration
	// Attempts is the number of route executions dispatched for the request.
	Attempts int
}

// SelectStream executes the bounded streaming route and returns the winner.
// The winner is chosen only by the first meaningful content, reasoning, or
// tool-call event; losers are cancelled and never affect health or lease state.
func (r *Runner) SelectStream(ctx context.Context, logical string, request ExecuteRequest) (*SelectedStream, *CallError) {
	outcome := r.runPlan(ctx, logical, request, true)
	if outcome.err != nil {
		return nil, outcome.err
	}
	outcome.selected.TTFT = outcome.ttft
	outcome.selected.Attempts = outcome.attempts
	return outcome.selected, nil
}
