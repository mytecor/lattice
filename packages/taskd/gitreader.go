package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// GitReader abstracts reading and scanning tasks from a Git repository.
type GitReader interface {
	ScanTasksAtCommit(ctx context.Context, repoPath, commit string) ([]*taskcontract.TaskSpec, error)
	ReadTaskAtCommit(ctx context.Context, repoPath, path, commit string) (*taskcontract.TaskSpec, error)
}

// GoGitReader reads task specifications directly from local Git objects.
type GoGitReader struct{}

// NewGoGitReader creates a new GoGitReader.
func NewGoGitReader() *GoGitReader {
	return &GoGitReader{}
}

// isTaskPath checks if a path is considered a potential task specification document.
func isTaskPath(path string) bool {
	clean := filepath.Clean(path)
	if clean == "task.md" {
		return true
	}
	if strings.HasPrefix(clean, "tasks/") && strings.HasSuffix(clean, ".md") {
		return true
	}
	return false
}

// ReadTaskAtCommit reads and parses a specific task document at the given commit.
func (r *GoGitReader) ReadTaskAtCommit(ctx context.Context, repoPath, path, commitHash string) (*taskcontract.TaskSpec, error) {
	if repoPath == "" {
		return nil, errors.New("empty repository path")
	}
	if commitHash == "" {
		return nil, errors.New("empty commit hash")
	}

	cleanRepo := filepath.Clean(repoPath)
	repo, err := git.PlainOpenWithOptions(cleanRepo, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("open repository %s: %w", cleanRepo, err)
	}

	c, err := repo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return nil, fmt.Errorf("commit %s not found: %w", commitHash, err)
	}

	tree, err := c.Tree()
	if err != nil {
		return nil, fmt.Errorf("tree for commit %s: %w", commitHash, err)
	}

	file, err := tree.File(path)
	if err != nil {
		return nil, fmt.Errorf("file %s in commit %s: %w", path, commitHash, err)
	}

	content, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	spec, err := taskcontract.ParseTask(cleanRepo, path, commitHash, []byte(content))
	if err != nil {
		return nil, fmt.Errorf("parse task %s at %s: %w", path, commitHash, err)
	}

	return spec, nil
}

// ScanTasksAtCommit scans all task documents at the given commit in repoPath.
func (r *GoGitReader) ScanTasksAtCommit(ctx context.Context, repoPath, commitHash string) ([]*taskcontract.TaskSpec, error) {
	if repoPath == "" {
		return nil, errors.New("empty repository path")
	}
	if commitHash == "" {
		return nil, errors.New("empty commit hash")
	}

	cleanRepo := filepath.Clean(repoPath)
	repo, err := git.PlainOpenWithOptions(cleanRepo, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("open repository %s: %w", cleanRepo, err)
	}

	c, err := repo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return nil, fmt.Errorf("commit %s not found: %w", commitHash, err)
	}

	tree, err := c.Tree()
	if err != nil {
		return nil, fmt.Errorf("tree for commit %s: %w", commitHash, err)
	}

	var specs []*taskcontract.TaskSpec
	err = tree.Files().ForEach(func(f *object.File) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !isTaskPath(f.Name) {
			return nil
		}

		content, err := f.Contents()
		if err != nil {
			return nil
		}

		spec, err := taskcontract.ParseTask(cleanRepo, f.Name, commitHash, []byte(content))
		if err == nil && spec != nil {
			specs = append(specs, spec)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("scan tree files: %w", err)
	}

	return specs, nil
}
