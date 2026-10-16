package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

func setupTestTaskRepo(t *testing.T) (string, string) {
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

	tasksDir := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}

	taskContent := `---
id: task-pub-01
source:
  repo: /tmp/source
  revision: main
runtime:
  image: default
---

# Task 01

## Context

Some background context.

## Requirements

Implement feature X.

## Acceptance criteria

Feature X is working.
`
	taskPath := filepath.Join(tasksDir, "task-01.md")
	if err := os.WriteFile(taskPath, []byte(taskContent), 0644); err != nil {
		t.Fatalf("failed to write task file: %v", err)
	}

	if _, err := wt.Add("tasks/task-01.md"); err != nil {
		t.Fatalf("failed to add task file: %v", err)
	}

	hash, err := wt.Commit("add task-01", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Author",
			Email: "author@example.com",
			When:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	return dir, hash.String()
}

func TestPublishResultSuccess(t *testing.T) {
	repoDir, commitHash := setupTestTaskRepo(t)
	publisher := NewGoGitPublisher()

	spec := &taskcontract.TaskSpec{
		ID:                 "task-pub-01",
		TaskRepo:           repoDir,
		Path:               "tasks/task-01.md",
		Commit:             commitHash,
		Context:            "Some background context.",
		Requirements:       "Implement feature X.",
		AcceptanceCriteria: "Feature X is working.",
	}

	result := &taskcontract.TaskResult{
		Status:           taskcontract.StatusCompleted,
		Revision:         "rev-dummy-123",
		ResultText:       "Task completed successfully.",
		VerificationText: "All checks passed.",
	}

	ctx := context.Background()
	err := publisher.PublishResult(ctx, repoDir, spec, result)
	if err != nil {
		t.Fatalf("PublishResult failed: %v", err)
	}

	// Verify task.md in repo
	taskFilePath := filepath.Join(repoDir, "tasks", "task-01.md")
	updatedContent, err := os.ReadFile(taskFilePath)
	if err != nil {
		t.Fatalf("failed to read updated task file: %v", err)
	}

	parsed, err := taskcontract.ParseTaskDocument(updatedContent)
	if err != nil {
		t.Fatalf("failed to parse updated task document: %v", err)
	}

	if parsed.IsActionable() {
		t.Errorf("expected task to be non-actionable after result publication")
	}

	if parsed.Result == nil {
		t.Fatalf("expected non-nil result")
	}

	if parsed.Result.Status != taskcontract.StatusCompleted {
		t.Errorf("expected status %s, got %s", taskcontract.StatusCompleted, parsed.Result.Status)
	}

	if parsed.Result.Revision != "rev-dummy-123" {
		t.Errorf("expected revision rev-dummy-123, got %s", parsed.Result.Revision)
	}

	strContent := string(updatedContent)
	if !strings.Contains(strContent, "## Result") {
		t.Errorf("expected ## Result in document: %s", strContent)
	}
	if !strings.Contains(strContent, "Task completed successfully.") {
		t.Errorf("expected result text in document: %s", strContent)
	}
	if !strings.Contains(strContent, "## Verification") {
		t.Errorf("expected ## Verification in document: %s", strContent)
	}
}

func TestPublishResultIdempotent(t *testing.T) {
	repoDir, commitHash := setupTestTaskRepo(t)
	publisher := NewGoGitPublisher()

	spec := &taskcontract.TaskSpec{
		ID:                 "task-pub-01",
		TaskRepo:           repoDir,
		Path:               "tasks/task-01.md",
		Commit:             commitHash,
		Context:            "Some background context.",
		Requirements:       "Implement feature X.",
		AcceptanceCriteria: "Feature X is working.",
	}

	result := &taskcontract.TaskResult{
		Status:           taskcontract.StatusCompleted,
		Revision:         "rev-dummy-123",
		ResultText:       "Task completed successfully.",
		VerificationText: "All checks passed.",
	}

	ctx := context.Background()

	// First publication
	if err := publisher.PublishResult(ctx, repoDir, spec, result); err != nil {
		t.Fatalf("first publish failed: %v", err)
	}

	repo, _ := git.PlainOpen(repoDir)
	head1, _ := repo.Head()

	// Second publication (exact replay)
	if err := publisher.PublishResult(ctx, repoDir, spec, result); err != nil {
		t.Fatalf("second publish failed: %v", err)
	}

	head2, _ := repo.Head()
	if head1.Hash() != head2.Hash() {
		t.Errorf("idempotent publication created redundant commit: %s vs %s", head1.Hash(), head2.Hash())
	}
}

func TestPublishResultConcurrentModificationConflict(t *testing.T) {
	repoDir, commitHash := setupTestTaskRepo(t)
	publisher := NewGoGitPublisher()

	spec := &taskcontract.TaskSpec{
		ID:                 "task-pub-01",
		TaskRepo:           repoDir,
		Path:               "tasks/task-01.md",
		Commit:             commitHash,
		Context:            "Some background context.",
		Requirements:       "Implement feature X.",
		AcceptanceCriteria: "Feature X is working.",
	}

	// Concurrently modify requirements in the file
	taskFilePath := filepath.Join(repoDir, "tasks", "task-01.md")
	newContent := `---
id: task-pub-01
source:
  repo: /tmp/source
  revision: main
runtime:
  image: default
---

# Task 01

## Context

Some background context.

## Requirements

Implement feature Y instead of feature X!

## Acceptance criteria

Feature Y is working.
`
	_ = os.WriteFile(taskFilePath, []byte(newContent), 0644)

	result := &taskcontract.TaskResult{
		Status:   taskcontract.StatusCompleted,
		Revision: "rev-dummy-123",
	}

	ctx := context.Background()
	err := publisher.PublishResult(ctx, repoDir, spec, result)
	if err == nil {
		t.Fatalf("expected conflict error when requirements were modified concurrently, got nil")
	}

	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected conflict in error message, got: %v", err)
	}

	// Verify the new requirements were not overwritten
	currentBytes, _ := os.ReadFile(taskFilePath)
	if !strings.Contains(string(currentBytes), "Implement feature Y") {
		t.Errorf("concurrent modification was overwritten by stale result!")
	}
}
