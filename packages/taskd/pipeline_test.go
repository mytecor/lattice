package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	taskcontract "github.com/mytecor/lattice/task-contract"
	"github.com/mytecor/meshbus/core"
)

// In-process shared bus for pipeline integration testing
type testBus struct {
	mu          sync.Mutex
	subscribers map[string][]func(context.Context, core.ReceivedEvent) error
}

func newTestBus() *testBus {
	return &testBus{
		subscribers: make(map[string][]func(context.Context, core.ReceivedEvent) error),
	}
}

type testSub struct {
	onClose func()
}

func (s *testSub) Close() error {
	if s.onClose != nil {
		s.onClose()
	}
	return nil
}

func (b *testBus) Subscribe(pattern string, handler func(context.Context, core.ReceivedEvent) error) (io.Closer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[pattern] = append(b.subscribers[pattern], handler)
	return &testSub{
		onClose: func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subscribers, pattern)
		},
	}, nil
}

func (b *testBus) Publish(ctx context.Context, topic string, payload []byte, opts core.PublishOptions) error {
	b.mu.Lock()
	var matchingHandlers []func(context.Context, core.ReceivedEvent) error
	for pattern, handlers := range b.subscribers {
		if pattern == topic || strings.HasPrefix(pattern, topic) || strings.HasPrefix(topic, strings.TrimSuffix(pattern, "*")) {
			matchingHandlers = append(matchingHandlers, handlers...)
		}
	}
	b.mu.Unlock()

	for _, h := range matchingHandlers {
		go func(fn func(context.Context, core.ReceivedEvent) error) {
			_ = fn(ctx, core.ReceivedEvent{
				Event: core.Event{
					Topic:   topic,
					Payload: payload,
				},
			})
		}(h)
	}
	return nil
}

// Minimal in-memory executor for pipeline testing
type dummyExecutor struct {
	workDir string
}

func (e *dummyExecutor) ExecuteTask(ctx context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	// Simple deterministic execution: open source repo and add dummy file
	repo, err := git.PlainOpen(spec.Source.Repo)
	if err != nil {
		return nil, fmt.Errorf("open source repo: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("worktree: %w", err)
	}

	taskFilePath := filepath.Join(spec.Source.Repo, ".lattice-tasks", fmt.Sprintf("%s.json", spec.ID))

	// Check if already exists (idempotency)
	if existing, err := os.ReadFile(taskFilePath); err == nil {
		var data map[string]string
		if jErr := json.Unmarshal(existing, &data); jErr == nil && data["task_id"] == spec.ID {
			head, _ := repo.Head()
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusCompleted,
				Revision:         head.Hash().String(),
				ResultText:       fmt.Sprintf("Idempotent replay: %s", head.Hash().String()),
				VerificationText: "Idempotent dummy replay verified",
			}, nil
		}
	}

	_ = os.MkdirAll(filepath.Dir(taskFilePath), 0755)
	payload, _ := json.Marshal(map[string]string{
		"task_id":     spec.ID,
		"task_commit": spec.Commit,
	})
	if err := os.WriteFile(taskFilePath, payload, 0644); err != nil {
		return nil, err
	}

	wt.Add(fmt.Sprintf(".lattice-tasks/%s.json", spec.ID))
	commitHash, err := wt.Commit(fmt.Sprintf("dummy: complete %s", spec.ID), &git.CommitOptions{
		Author: &object.Signature{Name: "agent", Email: "agent@lattice.local", When: time.Now()},
	})
	if err != nil {
		return nil, err
	}

	return &taskcontract.TaskResult{
		Status:           taskcontract.StatusCompleted,
		Revision:         commitHash.String(),
		ResultText:       fmt.Sprintf("Dummy completed: commit %s", commitHash.String()),
		VerificationText: "Deterministic execution verified",
	}, nil
}

func setupPipelineRepos(t *testing.T) (taskRepoDir, sourceRepoDir, initTaskCommit, initSourceCommit string) {
	t.Helper()

	// 1. Source repo
	sourceRepoDir = t.TempDir()
	sRepo, err := git.PlainInit(sourceRepoDir, false)
	if err != nil {
		t.Fatalf("init source repo: %v", err)
	}
	sWt, _ := sRepo.Worktree()
	_ = os.WriteFile(filepath.Join(sourceRepoDir, "README.md"), []byte("# Source\n"), 0644)
	sWt.Add("README.md")
	sHash, _ := sWt.Commit("initial source commit", &git.CommitOptions{
		Author: &object.Signature{Name: "init", Email: "init@local", When: time.Unix(0, 0)},
	})
	initSourceCommit = sHash.String()

	// 2. Task repo
	taskRepoDir = t.TempDir()
	tRepo, err := git.PlainInit(taskRepoDir, false)
	if err != nil {
		t.Fatalf("init task repo: %v", err)
	}
	tWt, _ := tRepo.Worktree()
	_ = os.MkdirAll(filepath.Join(taskRepoDir, "tasks"), 0755)

	taskContent := fmt.Sprintf(`---
id: task-pipeline-01
source:
  repo: %s
  revision: %s
runtime:
  image: default
---

# Pipeline Task 01

## Context

End-to-end pipeline test.

## Requirements

Produce deterministic commit.

## Acceptance criteria

Commit created and recorded in Git.
`, sourceRepoDir, initSourceCommit)

	_ = os.WriteFile(filepath.Join(taskRepoDir, "tasks", "task-01.md"), []byte(taskContent), 0644)
	tWt.Add("tasks/task-01.md")
	tHash, _ := tWt.Commit("add task-01", &git.CommitOptions{
		Author: &object.Signature{Name: "planner", Email: "planner@local", When: time.Unix(0, 0)},
	})
	initTaskCommit = tHash.String()

	return taskRepoDir, sourceRepoDir, initTaskCommit, initSourceCommit
}

type localBusAdapter struct {
	bus *testBus
}

func (a *localBusAdapter) Subscribe(pattern string, handler core.EventHandler) (*core.Subscription, error) {
	// Not needed for direct agent caller test
	return nil, nil
}

func (a *localBusAdapter) Publish(ctx context.Context, topic string, payload []byte, opts core.PublishOptions) (core.PublishResult, error) {
	err := a.bus.Publish(ctx, topic, payload, opts)
	return core.PublishResult{}, err
}

func TestPipelineEndToEndDummyWorkload(t *testing.T) {
	taskRepoDir, sourceRepoDir, initTaskCommit, _ := setupPipelineRepos(t)

	// Mock git watcher that returns the initial task commit
	gitWatcher := &mockGitWatcher{
		headRevision: initTaskCommit,
		watchResp:    &WatchResponse{Status: StatusNoChange},
	}
	gitReader := NewGoGitReader()
	gitPublisher := NewGoGitPublisher()

	// Direct agent caller using dummyExecutor
	exec := &dummyExecutor{workDir: t.TempDir()}

	caller := &directAgentCaller{
		exec: exec,
	}

	resultPublishedCh := make(chan struct{}, 1)
	reconciler, err := NewReconciler(ReconcilerConfig{
		RepoPath:     taskRepoDir,
		Ref:          "HEAD",
		GitWatcher:   gitWatcher,
		GitReader:    gitReader,
		AgentCaller:  caller,
		WatchTimeout: 100 * time.Millisecond,
		ResultHandler: func(ctx context.Context, spec *taskcontract.TaskSpec, result *taskcontract.TaskResult) error {
			defer func() {
				select {
				case resultPublishedCh <- struct{}{}:
				default:
				}
			}()
			return gitPublisher.PublishResult(ctx, taskRepoDir, spec, result)
		},
	})
	if err != nil {
		t.Fatalf("failed to create reconciler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = reconciler.Run(ctx)
	}()

	// Wait for task completion and publication
	select {
	case <-resultPublishedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pipeline result publication")
	}

	// Verify task.md in taskRepo was updated with terminal result
	taskContent, err := os.ReadFile(filepath.Join(taskRepoDir, "tasks", "task-01.md"))
	if err != nil {
		t.Fatalf("failed to read task file: %v", err)
	}

	parsed, err := taskcontract.ParseTaskDocument(taskContent)
	if err != nil {
		t.Fatalf("parse task document: %v", err)
	}

	if parsed.IsActionable() {
		t.Errorf("task should be non-actionable after pipeline completion")
	}

	if parsed.Result == nil || parsed.Result.Status != taskcontract.StatusCompleted {
		t.Fatalf("expected status completed, got %v", parsed.Result)
	}

	if parsed.Result.Revision == "" {
		t.Fatalf("expected result revision to be set")
	}

	// Verify source repo contains the committed file
	sourceFile := filepath.Join(sourceRepoDir, ".lattice-tasks", "task-pipeline-01.json")
	if _, err := os.Stat(sourceFile); os.IsNotExist(err) {
		t.Fatalf("expected dummy file %s to exist in source repository", sourceFile)
	}
}

func TestPipelineRecoveryDrillAfterTaskdRestart(t *testing.T) {
	taskRepoDir, _, initTaskCommit, _ := setupPipelineRepos(t)

	gitWatcher := &mockGitWatcher{
		headRevision: initTaskCommit,
		watchResp:    &WatchResponse{Status: StatusNoChange},
	}
	gitReader := NewGoGitReader()
	gitPublisher := NewGoGitPublisher()
	exec := &dummyExecutor{workDir: t.TempDir()}

	// Scenario: First taskd instance crashes immediately after starting
	caller1 := &blockingAgentCaller{
		blockCh: make(chan struct{}),
	}

	r1, err := NewReconciler(ReconcilerConfig{
		RepoPath:     taskRepoDir,
		Ref:          "HEAD",
		GitWatcher:   gitWatcher,
		GitReader:    gitReader,
		AgentCaller:  caller1,
		WatchTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create r1: %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() {
		_ = r1.Run(ctx1)
	}()

	// Allow startup scan to find the task
	time.Sleep(50 * time.Millisecond)

	// Simulate sudden crash: cancel context and destroy r1
	cancel1()
	r1.Stop()

	// Task was NOT completed; task.md is still actionable.
	// Now start a brand new taskd (simulating daemon restart with clean memory)
	resultPublishedCh := make(chan struct{}, 1)
	caller2 := &directAgentCaller{exec: exec}

	r2, err := NewReconciler(ReconcilerConfig{
		RepoPath:     taskRepoDir,
		Ref:          "HEAD",
		GitWatcher:   gitWatcher,
		GitReader:    gitReader,
		AgentCaller:  caller2,
		WatchTimeout: 100 * time.Millisecond,
		ResultHandler: func(ctx context.Context, spec *taskcontract.TaskSpec, result *taskcontract.TaskResult) error {
			defer func() {
				select {
				case resultPublishedCh <- struct{}{}:
				default:
				}
			}()
			return gitPublisher.PublishResult(ctx, taskRepoDir, spec, result)
		},
	})
	if err != nil {
		t.Fatalf("failed to create r2: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	go func() {
		_ = r2.Run(ctx2)
	}()

	// Startup scan of r2 MUST find the actionable task and complete it
	select {
	case <-resultPublishedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for task to complete after taskd restart")
	}

	taskContent, err := os.ReadFile(filepath.Join(taskRepoDir, "tasks", "task-01.md"))
	if err != nil {
		t.Fatalf("read task file: %v", err)
	}

	parsed, _ := taskcontract.ParseTaskDocument(taskContent)
	if parsed.IsActionable() {
		t.Errorf("task should have completed after restart")
	}
}

type directAgentCaller struct {
	exec *dummyExecutor
}

func (c *directAgentCaller) RunTask(ctx context.Context, spec taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	return c.exec.ExecuteTask(ctx, &spec)
}

type blockingAgentCaller struct {
	blockCh chan struct{}
}

func (c *blockingAgentCaller) RunTask(ctx context.Context, spec taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.blockCh:
		return nil, nil
	}
}
