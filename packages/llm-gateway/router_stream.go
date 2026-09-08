package main

import (
	"context"
	"time"
)

type SelectedStream struct {
	Buffered  []StreamEvent
	Remaining <-chan StreamEvent
	Cancel    context.CancelFunc
}

type streamResult struct {
	provider string
	buffered []StreamEvent
	stream   <-chan StreamEvent
	cancel   context.CancelFunc
	err      *CallError
}

func (r *Runner) SelectStream(ctx context.Context, logical string, request ExecuteRequest) (*SelectedStream, *CallError) {
	plan, ok := r.config.plans[logical]
	if !ok {
		return nil, &CallError{Class: ErrorInvalid, Status: 404}
	}
	var last *CallError
	for index, stage := range plan.Stages {
		selected, callErr := r.selectStreamStage(ctx, logical, index+1, stage, request)
		if callErr == nil {
			return selected, nil
		}
		last = callErr
		if index == len(plan.Stages)-1 || !stage.NextOn[callErr.Class] {
			break
		}
	}
	return nil, last
}

func (r *Runner) selectStreamStage(ctx context.Context, logical string, stageIndex int, stage Stage, request ExecuteRequest) (*SelectedStream, *CallError) {
	var last *CallError
	for attempt := 0; attempt <= stage.Retries; attempt++ {
		stageCtx := withRouteAttempt(ctx, stageIndex, attempt+1)
		selected, callErr := r.selectStreamAttempt(stageCtx, logical, stage, request)
		if callErr == nil {
			return selected, nil
		}
		last = callErr
		if attempt == stage.Retries || !stage.RetryOn[callErr.Class] {
			break
		}
		if err := r.sleep(ctx, backoffDuration(stage.Backoff, attempt)); err != nil {
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: err}
		}
	}
	return nil, last
}

func (r *Runner) selectStreamAttempt(ctx context.Context, logical string, stage Stage, request ExecuteRequest) (*SelectedStream, *CallError) {
	providers := r.availableProviders(stage.Providers)
	if len(providers) == 0 {
		providers = append([]string(nil), stage.Providers...)
	}
	timeout, stopTimeout := streamSelectionTimer(stage.Timeout)
	defer stopTimeout()
	if stage.Mode == "serial" {
		var last *CallError
		for _, providerID := range providers {
			branchCtx, cancel := context.WithCancel(ctx)
			results := make(chan streamResult, 1)
			go func() {
				results <- r.probeStream(branchCtx, cancel, logical, providerID, request)
			}()
			select {
			case result := <-results:
				r.record(providerID, result.err)
				if result.err == nil {
					return &SelectedStream{Buffered: result.buffered, Remaining: result.stream, Cancel: cancel}, nil
				}
				cancel()
				last = result.err
			case <-timeout:
				cancel()
				callErr := streamSelectionTimeoutError()
				r.record(providerID, callErr)
				return nil, callErr
			case <-ctx.Done():
				cancel()
				return nil, streamSelectionContextError(ctx)
			}
		}
		return nil, last
	}

	results := make(chan streamResult, len(providers))
	cancels := make([]context.CancelFunc, len(providers))
	for index, providerID := range providers {
		branchCtx, cancel := context.WithCancel(ctx)
		cancels[index] = cancel
		index, providerID := index, providerID
		go func() {
			if stage.Mode == "hedge" && index > 0 {
				if err := sleepContext(branchCtx, time.Duration(index)*stage.HedgeDelay); err != nil {
					results <- streamResult{provider: providerID, cancel: cancel, err: &CallError{Class: ErrorCancelled, Status: 499, Cause: err}}
					return
				}
			}
			result := r.probeStream(branchCtx, cancel, logical, providerID, request)
			results <- result
		}()
	}

	var last *CallError
	for range providers {
		select {
		case result := <-results:
			r.record(result.provider, result.err)
			if result.err == nil {
				for index, providerID := range providers {
					if providerID != result.provider {
						cancels[index]()
					}
				}
				return &SelectedStream{
					Buffered: result.buffered, Remaining: result.stream, Cancel: result.cancel,
				}, nil
			}
			result.cancel()
			if result.err.Class != ErrorCancelled || last == nil {
				last = result.err
			}
		case <-timeout:
			callErr := streamSelectionTimeoutError()
			for index, providerID := range providers {
				cancels[index]()
				r.record(providerID, callErr)
			}
			return nil, callErr
		case <-ctx.Done():
			for _, cancel := range cancels {
				cancel()
			}
			return nil, streamSelectionContextError(ctx)
		}
	}
	if last == nil {
		last = &CallError{Class: ErrorInvalid, Status: 502}
	}
	return nil, last
}

func streamSelectionTimer(timeout time.Duration) (<-chan time.Time, func()) {
	if timeout <= 0 {
		return nil, func() {}
	}
	timer := time.NewTimer(timeout)
	return timer.C, func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

func streamSelectionTimeoutError() *CallError {
	return &CallError{Class: ErrorTimeout, Status: 504, Cause: context.DeadlineExceeded}
}

func streamSelectionContextError(ctx context.Context) *CallError {
	if ctx.Err() == context.DeadlineExceeded {
		return streamSelectionTimeoutError()
	}
	return &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
}

func (r *Runner) probeStream(ctx context.Context, cancel context.CancelFunc, logical, providerID string, request ExecuteRequest) streamResult {
	target, callErr := r.target(logical, providerID)
	if callErr != nil {
		return streamResult{provider: providerID, cancel: cancel, err: callErr}
	}
	stream, callErr := r.executor.Stream(ctx, target, request)
	if callErr != nil {
		return streamResult{provider: providerID, cancel: cancel, err: callErr}
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
			return streamResult{provider: providerID, cancel: cancel, err: &CallError{Class: class, Status: status, Cause: ctx.Err()}}
		case event, ok := <-stream:
			if !ok {
				return streamResult{provider: providerID, cancel: cancel, err: &CallError{Class: ErrorInvalid, Status: 502}}
			}
			if event.Err != nil {
				return streamResult{provider: providerID, cancel: cancel, err: normalizeContextError(ctx, event.Err)}
			}
			buffered = append(buffered, event)
			if event.Meaningful {
				return streamResult{provider: providerID, buffered: buffered, stream: stream, cancel: cancel}
			}
		}
	}
}
