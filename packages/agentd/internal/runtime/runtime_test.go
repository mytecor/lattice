package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

func TestInMemRuntime_Contract(t *testing.T) {
	rt := NewInMemRuntime()
	ctx := context.Background()

	spec := &taskcontract.TaskSpec{
		ID: "task-inmem-01",
	}

	inst, err := rt.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if inst.Endpoint() == "" {
		t.Fatal("expected non-empty endpoint")
	}
	if inst.Commit() == "" {
		t.Fatal("expected non-empty commit hash")
	}

	if err := inst.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestDummyRuntime_Contract(t *testing.T) {
	tempBase := t.TempDir()
	sourceRepoDir := filepath.Join(tempBase, "source-repo")
	if err := os.MkdirAll(sourceRepoDir, 0755); err != nil {
		t.Fatalf("failed to create source repo dir: %v", err)
	}

	// Initialize source repo with a commit
	repo, err := git.PlainInit(sourceRepoDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	initFile := filepath.Join(sourceRepoDir, "README.md")
	if err := os.WriteFile(initFile, []byte("# Source\n"), 0644); err != nil {
		t.Fatalf("write init file: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	initCommit, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Unix(100, 0),
		},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}

	rt := NewDummyRuntime(tempBase)
	ctx := context.Background()

	spec := &taskcontract.TaskSpec{
		ID:         "task-dummy-01",
		TaskRepo:   "/var/lib/tasks",
		Path:       "tasks/task-01.md",
		Commit:     "rev-01",
		Source:     taskcontract.SourceSpec{Repo: sourceRepoDir, Revision: initCommit.String()},
		Runtime:    taskcontract.RuntimeSpec{Image: "alpine:latest"},
	}

	inst, err := rt.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if inst.Endpoint() == "" {
		t.Error("expected non-empty endpoint")
	}
	if inst.Commit() == "" {
		t.Error("expected non-empty commit")
	}
	if inst.WorkDir() == "" {
		t.Error("expected non-empty WorkDir")
	}

	// Verify deterministic file was created in workDir
	detFile := filepath.Join(inst.WorkDir(), ".lattice-tasks", "task-dummy-01.json")
	if _, err := os.Stat(detFile); os.IsNotExist(err) {
		t.Errorf("expected deterministic file %s to exist", detFile)
	}

	workDir := inst.WorkDir()
	if err := inst.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify temp workDir is removed on Stop
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("expected workDir %s to be cleaned up after Stop", workDir)
	}
}
