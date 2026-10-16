package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\noutput: %s", args, err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.name", "Test")
	runGit("config", "user.email", "test@example.com")

	// Commit 1
	f1 := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f1, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "-m", "init")

	return dir
}

func addCommit(t *testing.T, dir, filename, content, message string) {
	t.Helper()
	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\noutput: %s", args, err, string(out))
		}
	}

	p := filepath.Join(dir, filename)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", filename)
	runGit("commit", "-m", message)
}

func TestValidateLocalRepo(t *testing.T) {
	ctx := context.Background()
	engine := NewLocalGitEngine("", "")

	// Empty repo
	if err := engine.ValidateLocalRepo(ctx, ""); err == nil {
		t.Error("expected error for empty repo, got nil")
	}

	// Remote repo URLs must be rejected
	remoteURLs := []string{
		"https://github.com/mytecor/lattice.git",
		"http://example.com/repo.git",
		"git@github.com:mytecor/lattice.git",
		"ssh://git@host/repo.git",
	}
	for _, u := range remoteURLs {
		if err := engine.ValidateLocalRepo(ctx, u); !errors.Is(err, ErrRemoteNotSupported) {
			t.Errorf("expected ErrRemoteNotSupported for %q, got: %v", u, err)
		}
	}

	// Non-existent directory
	if err := engine.ValidateLocalRepo(ctx, "/tmp/non-existent-dir-for-test-xyz-1234"); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("expected ErrRepoNotFound, got: %v", err)
	}

	// Non-git directory
	nonGitDir := t.TempDir()
	if err := engine.ValidateLocalRepo(ctx, nonGitDir); !errors.Is(err, ErrNotGitRepo) {
		t.Errorf("expected ErrNotGitRepo, got: %v", err)
	}

	// Valid git repo
	gitDir := setupTestGitRepo(t)
	if err := engine.ValidateLocalRepo(ctx, gitDir); err != nil {
		t.Errorf("unexpected error for valid repo: %v", err)
	}

	// Allowed prefix restrictions
	restrictedEngine := NewLocalGitEngine("", gitDir)
	if err := restrictedEngine.ValidateLocalRepo(ctx, gitDir); err != nil {
		t.Errorf("allowed dir rejected: %v", err)
	}
	otherDir := setupTestGitRepo(t)
	if err := restrictedEngine.ValidateLocalRepo(ctx, otherDir); !errors.Is(err, ErrPathOutsideAllowed) {
		t.Errorf("expected ErrPathOutsideAllowed, got: %v", err)
	}
}

func TestResolveHead(t *testing.T) {
	ctx := context.Background()
	engine := NewLocalGitEngine("", "")
	dir := setupTestGitRepo(t)

	// HEAD
	rev1, err := engine.ResolveHead(ctx, dir, "")
	if err != nil {
		t.Fatalf("ResolveHead HEAD failed: %v", err)
	}
	if len(rev1) != 40 {
		t.Errorf("expected 40-char SHA, got: %q", rev1)
	}

	// Named branch or explicit HEAD
	rev2, err := engine.ResolveHead(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("ResolveHead HEAD failed: %v", err)
	}
	if rev1 != rev2 {
		t.Errorf("expected rev1 == rev2, got %q != %q", rev1, rev2)
	}

	// Unknown ref
	_, err = engine.ResolveHead(ctx, dir, "refs/heads/non-existent-branch-123")
	if !errors.Is(err, ErrRefNotFound) {
		t.Errorf("expected ErrRefNotFound, got: %v", err)
	}
}

func TestDiffPaths(t *testing.T) {
	ctx := context.Background()
	engine := NewLocalGitEngine("", "")
	dir := setupTestGitRepo(t)

	rev1, err := engine.ResolveHead(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	// Initial commit diff (from == "")
	initPaths, err := engine.DiffPaths(ctx, dir, "", rev1)
	if err != nil {
		t.Fatalf("DiffPaths initial failed: %v", err)
	}
	if len(initPaths) != 1 || initPaths[0] != "README.md" {
		t.Errorf("expected [README.md], got: %v", initPaths)
	}

	// Commit 2: add a new file and modify existing
	addCommit(t, dir, "task.md", "---\nid: test\n---\n# Task\n", "add task")
	rev2, err := engine.ResolveHead(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	paths12, err := engine.DiffPaths(ctx, dir, rev1, rev2)
	if err != nil {
		t.Fatalf("DiffPaths rev1 -> rev2 failed: %v", err)
	}
	if len(paths12) != 1 || paths12[0] != "task.md" {
		t.Errorf("expected [task.md], got: %v", paths12)
	}

	// Commit 3: add multiple files
	addCommit(t, dir, "src/main.go", "package main\n", "add main.go")
	addCommit(t, dir, "src/util.go", "package main\n", "add util.go")
	rev3, err := engine.ResolveHead(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	paths23, err := engine.DiffPaths(ctx, dir, rev2, rev3)
	if err != nil {
		t.Fatalf("DiffPaths rev2 -> rev3 failed: %v", err)
	}
	if len(paths23) != 2 {
		t.Errorf("expected 2 files changed, got: %v", paths23)
	}

	// Check invalid from revision
	_, err = engine.DiffPaths(ctx, dir, "0123456789abcdef0123456789abcdef01234567", rev3)
	if err == nil {
		t.Error("expected error for non-existent from revision, got nil")
	}
}
