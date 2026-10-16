package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

type mockGitWatcher struct {
	mu            sync.Mutex
	headRevision  string
	headErr       error
	watchResp     *WatchResponse
	watchErr      error
	watchCalls    int
	watchCalledCh chan struct{}
}

func (m *mockGitWatcher) Head(ctx context.Context, repo, ref string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.headRevision, m.headErr
}

func (m *mockGitWatcher) Watch(ctx context.Context, repo, ref, afterRevision string, timeout time.Duration) (*WatchResponse, error) {
	m.mu.Lock()
	m.watchCalls++
	resp := m.watchResp
	err := m.watchErr
	m.mu.Unlock()

	if m.watchCalledCh != nil {
		select {
		case m.watchCalledCh <- struct{}{}:
		default:
		}
	}

	if resp == nil && err == nil {
		return &WatchResponse{Status: StatusNoChange}, nil
	}
	return resp, err
}

type mockAgentCaller struct {
	mu           sync.Mutex
	dispatched   []taskcontract.TaskSpec
	blockCh      chan struct{}
	resultToRet  *taskcontract.TaskResult
	errToRet     error
	dispatchedCh chan taskcontract.TaskSpec
}

func newMockAgentCaller() *mockAgentCaller {
	return &mockAgentCaller{
		resultToRet: &taskcontract.TaskResult{
			Status:   taskcontract.StatusCompleted,
			Revision: "deadbeef",
		},
		dispatchedCh: make(chan taskcontract.TaskSpec, 10),
	}
}

func (m *mockAgentCaller) RunTask(ctx context.Context, spec taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	m.mu.Lock()
	m.dispatched = append(m.dispatched, spec)
	block := m.blockCh
	res := m.resultToRet
	err := m.errToRet
	m.mu.Unlock()

	select {
	case m.dispatchedCh <- spec:
	default:
	}

	if block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-block:
		}
	}

	return res, err
}

func initTestRepo(t *testing.T) (string, string, string) {
	t.Helper()
	repoDir := t.TempDir()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v: %s", args, err, string(out))
		}
		return string(out)
	}

	run("init", "-b", "main")
	run("config", "user.name", "Test Reconciler")
	run("config", "user.email", "reconciler@example.com")

	if err := os.MkdirAll(filepath.Join(repoDir, "tasks"), 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Actionable task
	actionableTask := `---
id: task-01
source:
  repo: git://example/repo.git
  revision: 111111
runtime:
  image: alpine:latest
---
# Task 1
`
	if err := os.WriteFile(filepath.Join(repoDir, "tasks", "task-01.md"), []byte(actionableTask), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Terminal task
	terminalTask := `---
id: task-02
source:
  repo: git://example/repo.git
  revision: 222222
runtime:
  image: alpine:latest
result:
  status: completed
  revision: 333333
---
# Task 2
`
	if err := os.WriteFile(filepath.Join(repoDir, "tasks", "task-02.md"), []byte(terminalTask), 0644); err != nil {
		t.Fatal(err)
	}

	run("add", ".")
	run("commit", "-m", "initial tasks")
	initialCommit := run("rev-parse", "HEAD")
	initialCommit = initialCommit[:len(initialCommit)-1] // trim newline

	// 3. Make second commit updating task-01 to terminal
	actionableTaskCompleted := `---
id: task-01
source:
  repo: git://example/repo.git
  revision: 111111
runtime:
  image: alpine:latest
result:
  status: completed
  revision: 444444
---
# Task 1
`
	if err := os.WriteFile(filepath.Join(repoDir, "tasks", "task-01.md"), []byte(actionableTaskCompleted), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "completed task-01")
	completedCommit := run("rev-parse", "HEAD")
	completedCommit = completedCommit[:len(completedCommit)-1]

	return repoDir, initialCommit, completedCommit
}

func TestReconcilerStartupScanAndDeduplication(t *testing.T) {
	repoDir, initialCommit, _ := initTestRepo(t)

	mockWatcher := &mockGitWatcher{
		headRevision: initialCommit,
		watchResp:    &WatchResponse{Status: StatusNoChange},
	}
	agentCaller := newMockAgentCaller()

	reconciler, err := NewReconciler(ReconcilerConfig{
		RepoPath:     repoDir,
		Ref:          "HEAD",
		GitWatcher:   mockWatcher,
		AgentCaller:  agentCaller,
		WatchTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create reconciler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- reconciler.Run(ctx)
	}()

	// Wait for task-01 to be dispatched
	select {
	case spec := <-agentCaller.dispatchedCh:
		if spec.ID != "task-01" {
			t.Fatalf("expected task-01, got %s", spec.ID)
		}
		if spec.Commit != initialCommit {
			t.Fatalf("expected commit %s, got %s", initialCommit, spec.Commit)
		}
		if !spec.IsActionable() {
			t.Fatalf("dispatched spec should be actionable")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for task-01 dispatch on startup")
	}

	// Verify terminal task-02 was NOT dispatched
	agentCaller.mu.Lock()
	for _, spec := range agentCaller.dispatched {
		if spec.ID == "task-02" {
			t.Fatalf("task-02 is terminal and should not have been dispatched")
		}
	}
	agentCaller.mu.Unlock()

	cancel()
	<-errCh
}

func TestReconcilerRestartRecoversFromGit(t *testing.T) {
	repoDir, initialCommit, _ := initTestRepo(t)

	// Instance 1 runs and stops
	{
		mockWatcher := &mockGitWatcher{
			headRevision: initialCommit,
			watchResp:    &WatchResponse{Status: StatusNoChange},
		}
		agentCaller := newMockAgentCaller()

		reconciler, err := NewReconciler(ReconcilerConfig{
			RepoPath:     repoDir,
			Ref:          "HEAD",
			GitWatcher:   mockWatcher,
			AgentCaller:  agentCaller,
			WatchTimeout: 50 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- reconciler.Run(ctx)
		}()

		select {
		case <-agentCaller.dispatchedCh:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for dispatch")
		}
		cancel()
		<-errCh
	}

	// Instance 2 starts with fresh, empty in-memory state.
	// It must fully recover actionable work from Git startup scan!
	{
		mockWatcher := &mockGitWatcher{
			headRevision: initialCommit,
			watchResp:    &WatchResponse{Status: StatusNoChange},
		}
		agentCaller := newMockAgentCaller()

		reconciler, err := NewReconciler(ReconcilerConfig{
			RepoPath:     repoDir,
			Ref:          "HEAD",
			GitWatcher:   mockWatcher,
			AgentCaller:  agentCaller,
			WatchTimeout: 50 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- reconciler.Run(ctx)
		}()

		select {
		case spec := <-agentCaller.dispatchedCh:
			if spec.ID != "task-01" {
				t.Fatalf("expected task-01 recovered on restart, got %s", spec.ID)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("instance 2 failed to recover actionable tasks from Git")
		}
		cancel()
		<-errCh
	}
}

func TestReconcilerWatchRevisionChange(t *testing.T) {
	repoDir, _, completedCommit := initTestRepo(t)

	// Create commit 3: a brand new task-03.md
	task03 := `---
id: task-03
source:
  repo: git://example/repo.git
  revision: 555555
runtime:
  image: alpine:latest
---
# Task 3
`
	if err := os.WriteFile(filepath.Join(repoDir, "tasks", "task-03.md"), []byte(task03), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "add task-03")
	cmd.Dir = repoDir
	_ = cmd.Run()
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, _ := cmd.CombinedOutput()
	task03Commit := string(out)
	task03Commit = task03Commit[:len(task03Commit)-1]

	// Start at completedCommit (where task-01 and task-02 are both completed, so startup scan finds 0 actionable)
	watchCalledCh := make(chan struct{}, 10)
	mockWatcher := &mockGitWatcher{
		headRevision:  completedCommit,
		watchCalledCh: watchCalledCh,
	}
	agentCaller := newMockAgentCaller()

	reconciler, err := NewReconciler(ReconcilerConfig{
		RepoPath:     repoDir,
		Ref:          "HEAD",
		GitWatcher:   mockWatcher,
		AgentCaller:  agentCaller,
		WatchTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- reconciler.Run(ctx)
	}()

	// Wait for watch to be entered
	select {
	case <-watchCalledCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watch loop")
	}

	// Verify nothing was dispatched from startup scan of completedCommit
	agentCaller.mu.Lock()
	if len(agentCaller.dispatched) != 0 {
		t.Fatalf("expected 0 actionable tasks at completedCommit, got %d", len(agentCaller.dispatched))
	}
	agentCaller.mu.Unlock()

	// Now simulate revision change from completedCommit to task03Commit
	mockWatcher.mu.Lock()
	mockWatcher.watchResp = &WatchResponse{
		Status:       StatusChanged,
		From:         completedCommit,
		To:           task03Commit,
		ChangedPaths: []string{"tasks/task-03.md"},
	}
	mockWatcher.mu.Unlock()

	// Reconciler should catch this change and dispatch task-03
	select {
	case spec := <-agentCaller.dispatchedCh:
		if spec.ID != "task-03" {
			t.Fatalf("expected task-03, got %s", spec.ID)
		}
		if spec.Commit != task03Commit {
			t.Fatalf("expected commit %s, got %s", task03Commit, spec.Commit)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for task-03 dispatch after watch event")
	}

	cancel()
	<-errCh
}

func TestReconcilerSupersedesOlderRevision(t *testing.T) {
	repoDir, initialCommit, _ := initTestRepo(t)

	mockWatcher := &mockGitWatcher{
		headRevision: initialCommit,
		watchResp:    &WatchResponse{Status: StatusNoChange},
	}
	agentCaller := newMockAgentCaller()
	agentCaller.blockCh = make(chan struct{}) // Block execution

	reconciler, err := NewReconciler(ReconcilerConfig{
		RepoPath:     repoDir,
		Ref:          "HEAD",
		GitWatcher:   mockWatcher,
		AgentCaller:  agentCaller,
		WatchTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- reconciler.Run(ctx)
	}()

	// Wait for task-01 initial dispatch
	select {
	case <-agentCaller.dispatchedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for initial dispatch")
	}

	initialRevKey := (&taskcontract.TaskSpec{
		TaskRepo: repoDir,
		Path:     "tasks/task-01.md",
		Commit:   initialCommit,
	}).RevisionKey()

	if !reconciler.IsRunning(initialRevKey) {
		t.Fatalf("expected revision %s to be running", initialRevKey)
	}

	// Create new commit with edited task-01
	task01Updated := `---
id: task-01
source:
  repo: git://example/repo.git
  revision: 111111-updated
runtime:
  image: alpine:latest
---
# Task 1 Updated
`
	if err := os.WriteFile(filepath.Join(repoDir, "tasks", "task-01.md"), []byte(task01Updated), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "edit task-01")
	cmd.Dir = repoDir
	_ = cmd.Run()
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, _ := cmd.CombinedOutput()
	newCommit := string(out)
	newCommit = newCommit[:len(newCommit)-1]

	// Simulate watch notification for new revision
	mockWatcher.mu.Lock()
	mockWatcher.watchResp = &WatchResponse{
		Status:       StatusChanged,
		From:         initialCommit,
		To:           newCommit,
		ChangedPaths: []string{"tasks/task-01.md"},
	}
	mockWatcher.mu.Unlock()

	// Should dispatch the new revision and supersede the old one
	select {
	case spec := <-agentCaller.dispatchedCh:
		if spec.Commit != newCommit {
			t.Fatalf("expected new commit %s, got %s", newCommit, spec.Commit)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for updated revision dispatch")
	}

	// Verify old revision is no longer running in activeRuns
	if reconciler.IsRunning(initialRevKey) {
		t.Fatalf("expected old revision %s to be cancelled and superseded", initialRevKey)
	}

	close(agentCaller.blockCh)
	cancel()
	<-errCh
}
