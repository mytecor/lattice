package main

import (
	"context"
	"time"
)

const (
	DefaultTimeout      = 30 * time.Second
	MinTimeout          = 100 * time.Millisecond
	MaxTimeout          = 5 * time.Minute
	DefaultPollInterval = 250 * time.Millisecond
)

type Watcher struct {
	Engine       GitEngine
	PollInterval time.Duration
}

func NewWatcher(engine GitEngine, pollInterval time.Duration) *Watcher {
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	return &Watcher{
		Engine:       engine,
		PollInterval: pollInterval,
	}
}

// Watch handles a single watch request, returning immediately if changed,
// or long-polling until timeout/change.
func (w *Watcher) Watch(ctx context.Context, req WatchRequest) (*WatchResponse, error) {
	if err := w.Engine.ValidateLocalRepo(ctx, req.Repo); err != nil {
		return &WatchResponse{
			ID:     req.ID,
			Status: StatusError,
			Error:  err.Error(),
		}, nil
	}

	timeout := DefaultTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
		if timeout < MinTimeout {
			timeout = MinTimeout
		} else if timeout > MaxTimeout {
			timeout = MaxTimeout
		}
	}

	currentHead, err := w.Engine.ResolveHead(ctx, req.Repo, req.Ref)
	if err != nil {
		return &WatchResponse{
			ID:     req.ID,
			Status: StatusError,
			Error:  err.Error(),
		}, nil
	}

	// 1. Initial watch (no after_revision provided)
	if req.AfterRevision == "" {
		paths, err := w.Engine.DiffPaths(ctx, req.Repo, "", currentHead)
		if err != nil {
			return &WatchResponse{
				ID:     req.ID,
				Status: StatusError,
				Error:  err.Error(),
			}, nil
		}
		return &WatchResponse{
			ID:           req.ID,
			Status:       StatusChanged,
			From:         "",
			To:           currentHead,
			ChangedPaths: paths,
		}, nil
	}

	// 2. Cursor differs from current head
	if req.AfterRevision != currentHead {
		paths, err := w.Engine.DiffPaths(ctx, req.Repo, req.AfterRevision, currentHead)
		if err != nil {
			return &WatchResponse{
				ID:     req.ID,
				Status: StatusError,
				Error:  err.Error(),
			}, nil
		}
		return &WatchResponse{
			ID:           req.ID,
			Status:       StatusChanged,
			From:         req.AfterRevision,
			To:           currentHead,
			ChangedPaths: paths,
		}, nil
	}

	// 3. Cursor is current head: long-poll until change or timeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return &WatchResponse{
				ID:       req.ID,
				Status:   StatusNoChange,
				Revision: currentHead,
			}, nil
		case <-ticker.C:
			newHead, err := w.Engine.ResolveHead(ctx, req.Repo, req.Ref)
			if err != nil {
				continue
			}
			if newHead != currentHead {
				paths, err := w.Engine.DiffPaths(ctx, req.Repo, currentHead, newHead)
				if err != nil {
					return &WatchResponse{
						ID:     req.ID,
						Status: StatusError,
						Error:  err.Error(),
					}, nil
				}
				return &WatchResponse{
					ID:           req.ID,
					Status:       StatusChanged,
					From:         currentHead,
					To:           newHead,
					ChangedPaths: paths,
				}, nil
			}
		}
	}
}
