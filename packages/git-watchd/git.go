package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	ErrRemoteNotSupported = errors.New("remote repositories not supported; git-watchd only watches local repositories")
	ErrRepoNotFound       = errors.New("repository directory not found")
	ErrNotGitRepo         = errors.New("path is not a valid git repository")
	ErrRefNotFound        = errors.New("ref not found")
	ErrPathOutsideAllowed = errors.New("repository path outside allowed directory")
)

// GitEngine defines operations on local git repositories.
type GitEngine interface {
	ValidateLocalRepo(ctx context.Context, repo string) error
	ResolveHead(ctx context.Context, repo, ref string) (string, error)
	DiffPaths(ctx context.Context, repo, from, to string) ([]string, error)
}

// LocalGitEngine executes git CLI commands against local repositories.
type LocalGitEngine struct {
	GitBin        string
	AllowedPrefix string
}

// NewLocalGitEngine creates a LocalGitEngine.
func NewLocalGitEngine(gitBin, allowedPrefix string) *LocalGitEngine {
	if gitBin == "" {
		gitBin = "git"
	}
	if allowedPrefix != "" {
		allowedPrefix = filepath.Clean(allowedPrefix)
	}
	return &LocalGitEngine{
		GitBin:        gitBin,
		AllowedPrefix: allowedPrefix,
	}
}

func (e *LocalGitEngine) ValidateLocalRepo(ctx context.Context, repo string) error {
	if repo == "" {
		return errors.New("empty repository path")
	}

	// Reject remote URLs
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") || strings.Contains(repo, ":") && !strings.HasPrefix(repo, "/") && !strings.HasPrefix(repo, ".") {
		return fmt.Errorf("%w: %q", ErrRemoteNotSupported, repo)
	}

	cleanPath := filepath.Clean(repo)
	if e.AllowedPrefix != "" {
		rel, err := filepath.Rel(e.AllowedPrefix, cleanPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("%w: %s is not within %s", ErrPathOutsideAllowed, cleanPath, e.AllowedPrefix)
		}
	}

	fi, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrRepoNotFound, cleanPath)
		}
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrRepoNotFound, cleanPath)
	}

	cmd := exec.CommandContext(ctx, e.GitBin, "-C", cleanPath, "rev-parse", "--git-dir")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s (%s)", ErrNotGitRepo, cleanPath, strings.TrimSpace(string(out)))
	}

	return nil
}

func (e *LocalGitEngine) ResolveHead(ctx context.Context, repo, ref string) (string, error) {
	if err := e.ValidateLocalRepo(ctx, repo); err != nil {
		return "", err
	}
	if ref == "" {
		ref = "HEAD"
	}

	cleanPath := filepath.Clean(repo)
	// Peel to commit so tags or HEAD resolve to the underlying commit hash
	cmd := exec.CommandContext(ctx, e.GitBin, "-C", cleanPath, "rev-parse", "--verify", ref+"^{commit}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback to rev-parse without peeling if it's a symbolic ref or standard ref
		cmd2 := exec.CommandContext(ctx, e.GitBin, "-C", cleanPath, "rev-parse", "--verify", ref)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("%w: ref %q in %s (%s)", ErrRefNotFound, ref, cleanPath, strings.TrimSpace(string(out)))
		}
		out = out2
	}

	rev := strings.TrimSpace(string(out))
	if len(rev) < 7 {
		return "", fmt.Errorf("unexpected git rev-parse output: %q", rev)
	}
	return rev, nil
}

func (e *LocalGitEngine) DiffPaths(ctx context.Context, repo, from, to string) ([]string, error) {
	if err := e.ValidateLocalRepo(ctx, repo); err != nil {
		return nil, err
	}
	cleanPath := filepath.Clean(repo)

	if to == "" {
		return nil, errors.New("target revision 'to' cannot be empty")
	}

	var cmd *exec.Cmd
	if from == "" {
		// Root diff for initial commit: list all paths in the tree
		cmd = exec.CommandContext(ctx, e.GitBin, "-C", cleanPath, "diff-tree", "--root", "-r", "--no-commit-id", "--name-only", to)
	} else {
		// Check that 'from' object exists
		checkCmd := exec.CommandContext(ctx, e.GitBin, "-C", cleanPath, "cat-file", "-e", from)
		if checkOut, err := checkCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("after_revision %q does not exist in repository: %s", from, strings.TrimSpace(string(checkOut)))
		}

		// diff-tree between from and to (handles ancestor, diverged history, or arbitrary commits)
		cmd = exec.CommandContext(ctx, e.GitBin, "-C", cleanPath, "diff-tree", "-r", "--no-commit-id", "--name-only", from, to)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff-tree failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	lines := bytes.Split(out, []byte{'\n'})
	var paths []string
	seen := make(map[string]bool)
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			paths = append(paths, trimmed)
		}
	}
	if paths == nil {
		paths = []string{}
	}
	return paths, nil
}
