package main

import (
	"context"
	"testing"
	"time"
)

func TestWatcher(t *testing.T) {
	ctx := context.Background()
	dir := setupTestGitRepo(t)
	engine := NewGoGitEngine("")
	watcher := NewWatcher(engine, 50*time.Millisecond)

	rev1, err := engine.ResolveHead(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial watch (empty after_revision) -> immediate StatusChanged
	resp1, err := watcher.Watch(ctx, WatchRequest{
		ID:            "req-init",
		Repo:          dir,
		Ref:           "HEAD",
		AfterRevision: "",
		TimeoutMs:     500,
	})
	if err != nil {
		t.Fatalf("Watch initial failed: %v", err)
	}
	if resp1.Status != StatusChanged {
		t.Errorf("expected StatusChanged, got %s", resp1.Status)
	}
	if resp1.To != rev1 {
		t.Errorf("expected To == %s, got %s", rev1, resp1.To)
	}
	if len(resp1.ChangedPaths) != 1 || resp1.ChangedPaths[0] != "README.md" {
		t.Errorf("expected [README.md], got %v", resp1.ChangedPaths)
	}

	// 2. Head already advanced relative to after_revision -> immediate StatusChanged
	addCommit(t, dir, "file1.txt", "content1", "commit 2")
	rev2, err := engine.ResolveHead(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	resp2, err := watcher.Watch(ctx, WatchRequest{
		ID:            "req-behind",
		Repo:          dir,
		Ref:           "HEAD",
		AfterRevision: rev1,
		TimeoutMs:     500,
	})
	if err != nil {
		t.Fatalf("Watch behind failed: %v", err)
	}
	if resp2.Status != StatusChanged {
		t.Errorf("expected StatusChanged, got %s", resp2.Status)
	}
	if resp2.From != rev1 || resp2.To != rev2 {
		t.Errorf("expected from %s to %s, got from %s to %s", rev1, rev2, resp2.From, resp2.To)
	}
	if len(resp2.ChangedPaths) != 1 || resp2.ChangedPaths[0] != "file1.txt" {
		t.Errorf("expected [file1.txt], got %v", resp2.ChangedPaths)
	}

	// 3. Cursor matches head, no changes made -> StatusNoChange after timeout
	start := time.Now()
	resp3, err := watcher.Watch(ctx, WatchRequest{
		ID:            "req-nochange",
		Repo:          dir,
		Ref:           "HEAD",
		AfterRevision: rev2,
		TimeoutMs:     200, // 200ms
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Watch no_change failed: %v", err)
	}
	if resp3.Status != StatusNoChange {
		t.Errorf("expected StatusNoChange, got %s", resp3.Status)
	}
	if resp3.Revision != rev2 {
		t.Errorf("expected Revision == %s, got %s", rev2, resp3.Revision)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("expected wait of at least 150ms, elapsed: %v", elapsed)
	}

	// 4. Cursor matches head, change arrives while long-polling -> StatusChanged
	doneCh := make(chan *WatchResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := watcher.Watch(ctx, WatchRequest{
			ID:            "req-poll-change",
			Repo:          dir,
			Ref:           "HEAD",
			AfterRevision: rev2,
			TimeoutMs:     2000, // 2s
		})
		if err != nil {
			errCh <- err
		} else {
			doneCh <- resp
		}
	}()

	// Wait 100ms before committing to ensure watch loop is actively polling
	time.Sleep(100 * time.Millisecond)
	addCommit(t, dir, "file2.txt", "content2", "commit 3")
	rev3, err := engine.ResolveHead(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("Watch failed during long poll: %v", err)
	case resp := <-doneCh:
		if resp.Status != StatusChanged {
			t.Errorf("expected StatusChanged, got %s (error: %s)", resp.Status, resp.Error)
		}
		if resp.From != rev2 || resp.To != rev3 {
			t.Errorf("expected from %s to %s, got from %s to %s", rev2, rev3, resp.From, resp.To)
		}
		if len(resp.ChangedPaths) != 1 || resp.ChangedPaths[0] != "file2.txt" {
			t.Errorf("expected [file2.txt], got %v", resp.ChangedPaths)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watch response")
	}
}
