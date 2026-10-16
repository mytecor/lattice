package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

func setupTestGitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}

	testFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}

	hash, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	return dir, hash.String()
}

func TestDummyWorkloadCleanExecution(t *testing.T) {
	repoDir, initHash := setupTestGitRepo(t)
	executor := NewDummyWorkloadExecutor(t.TempDir())

	spec := &taskcontract.TaskSpec{
		ID:       "task-test-01",
		TaskRepo: "/var/lib/tasks",
		Path:     "tasks/task-01.md",
		Commit:   "abc12345",
		Source: taskcontract.SourceSpec{
			Repo:     repoDir,
			Revision: initHash,
		},
	}

	ctx := context.Background()
	result, err := executor.ExecuteTask(ctx, spec)
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	if result.Status != taskcontract.StatusCompleted {
		t.Fatalf("expected status %s, got %s (err: %s)",
			taskcontract.StatusCompleted, result.Status, result.ResultText)
	}

	if result.Revision == "" || result.Revision == initHash {
		t.Fatalf("expected new commit revision, got %s (init was %s)", result.Revision, initHash)
	}

	// Verify the commit exists in the source repository
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("failed to open repo: %v", err)
	}

	commitHash := plumbing.NewHash(result.Revision)
	commitObj, err := repo.CommitObject(commitHash)
	if err != nil {
		t.Fatalf("failed to get commit object for %s: %v", result.Revision, err)
	}

	tree, err := commitObj.Tree()
	if err != nil {
		t.Fatalf("failed to get tree: %v", err)
	}

	filePath := ".lattice-tasks/task-test-01.json"
	file, err := tree.File(filePath)
	if err != nil {
		t.Fatalf("expected file %s in commit tree: %v", filePath, err)
	}

	reader, err := file.Reader()
	if err != nil {
		t.Fatalf("failed to open file reader: %v", err)
	}
	defer reader.Close()

	var data DummyWorkloadData
	if err := json.NewDecoder(reader).Decode(&data); err != nil {
		t.Fatalf("failed to decode dummy workload JSON: %v", err)
	}

	if data.TaskID != "task-test-01" {
		t.Errorf("expected TaskID task-test-01, got %s", data.TaskID)
	}
	if data.TaskCommit != "abc12345" {
		t.Errorf("expected TaskCommit abc12345, got %s", data.TaskCommit)
	}
}

func TestDummyWorkloadIdempotentReplay(t *testing.T) {
	repoDir, initHash := setupTestGitRepo(t)
	executor := NewDummyWorkloadExecutor(t.TempDir())

	spec := &taskcontract.TaskSpec{
		ID:       "task-test-02",
		TaskRepo: "/var/lib/tasks",
		Path:     "tasks/task-02.md",
		Commit:   "def67890",
		Source: taskcontract.SourceSpec{
			Repo:     repoDir,
			Revision: initHash,
		},
	}

	ctx := context.Background()

	// 1. First execution
	res1, err := executor.ExecuteTask(ctx, spec)
	if err != nil {
		t.Fatalf("first execution failed: %v", err)
	}
	if res1.Status != taskcontract.StatusCompleted {
		t.Fatalf("expected status completed, got %s", res1.Status)
	}

	// 2. Second execution (replay after crash or restart)
	// TaskSpec points to the same target or branch
	spec.Source.Revision = res1.Revision
	res2, err := executor.ExecuteTask(ctx, spec)
	if err != nil {
		t.Fatalf("second execution failed: %v", err)
	}

	if res2.Status != taskcontract.StatusCompleted {
		t.Fatalf("expected status completed on replay, got %s (%s)", res2.Status, res2.ResultText)
	}

	if res2.Revision != res1.Revision {
		t.Errorf("expected identical revision on replay: %s vs %s", res1.Revision, res2.Revision)
	}
}

func TestDummyWorkloadConflictDetection(t *testing.T) {
	repoDir, initHash := setupTestGitRepo(t)
	executor := NewDummyWorkloadExecutor(t.TempDir())

	// Write conflicting file into repo
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	wt, _ := repo.Worktree()
	conflictingPath := filepath.Join(repoDir, ".lattice-tasks", "task-conflict.json")
	_ = os.MkdirAll(filepath.Dir(conflictingPath), 0755)
	_ = os.WriteFile(conflictingPath, []byte(`{"task_id":"different-id"}`), 0644)
	wt.Add(".lattice-tasks/task-conflict.json")
	conflictCommit, err := wt.Commit("add conflicting task", &git.CommitOptions{
		Author: &object.Signature{Name: "other", Email: "other@local", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("conflict commit: %v", err)
	}

	spec := &taskcontract.TaskSpec{
		ID:       "task-conflict",
		TaskRepo: "/var/lib/tasks",
		Path:     "tasks/task-conflict.md",
		Commit:   initHash,
		Source: taskcontract.SourceSpec{
			Repo:     repoDir,
			Revision: conflictCommit.String(),
		},
	}

	ctx := context.Background()
	result, err := executor.ExecuteTask(ctx, spec)
	if err != nil {
		t.Fatalf("expected non-error result with StatusBlocked, got err: %v", err)
	}

	if result.Status != taskcontract.StatusBlocked {
		t.Fatalf("expected status %s for conflict, got %s", taskcontract.StatusBlocked, result.Status)
	}
}

func TestDummyWorkloadVerificationChecks(t *testing.T) {
	repoDir, initHash := setupTestGitRepo(t)
	executor := NewDummyWorkloadExecutor(t.TempDir())

	// Test passing check
	specPass := &taskcontract.TaskSpec{
		ID:       "task-check-pass",
		TaskRepo: "/var/lib/tasks",
		Path:     "tasks/task-pass.md",
		Commit:   initHash,
		Source: taskcontract.SourceSpec{
			Repo:     repoDir,
			Revision: initHash,
		},
		Checks: []string{
			"test -f README.md",
			"grep 'Test Repo' README.md",
		},
	}

	ctx := context.Background()
	resPass, err := executor.ExecuteTask(ctx, specPass)
	if err != nil {
		t.Fatalf("pass check execution failed: %v", err)
	}
	if resPass.Status != taskcontract.StatusCompleted {
		t.Fatalf("expected status completed, got %s (%s)", resPass.Status, resPass.ResultText)
	}

	// Test failing check
	specFail := &taskcontract.TaskSpec{
		ID:       "task-check-fail",
		TaskRepo: "/var/lib/tasks",
		Path:     "tasks/task-fail.md",
		Commit:   initHash,
		Source: taskcontract.SourceSpec{
			Repo:     repoDir,
			Revision: initHash,
		},
		Checks: []string{
			"test -f non_existent_file.txt",
		},
	}

	resFail, err := executor.ExecuteTask(ctx, specFail)
	if err != nil {
		t.Fatalf("fail check execution returned error: %v", err)
	}
	if resFail.Status != taskcontract.StatusFailed {
		t.Fatalf("expected status %s for failed check, got %s", taskcontract.StatusFailed, resFail.Status)
	}
}
