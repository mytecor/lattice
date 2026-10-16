package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// GitPublisher publishes terminal TaskResults back into the task Git repository.
type GitPublisher interface {
	PublishResult(ctx context.Context, repoPath string, spec *taskcontract.TaskSpec, result *taskcontract.TaskResult) error
}

// GoGitPublisher implements GitPublisher using go-git.
type GoGitPublisher struct {
	mu sync.Mutex
}

// NewGoGitPublisher creates a new GoGitPublisher.
func NewGoGitPublisher() *GoGitPublisher {
	return &GoGitPublisher{}
}

// PublishResult writes the terminal result to task.md and commits it to the repository.
func (p *GoGitPublisher) PublishResult(ctx context.Context, repoPath string, spec *taskcontract.TaskSpec, result *taskcontract.TaskResult) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if spec == nil {
		return errors.New("cannot publish result for nil TaskSpec")
	}
	if result == nil {
		return errors.New("cannot publish nil TaskResult")
	}
	if err := result.Status.Validate(); err != nil {
		return fmt.Errorf("validate result status: %w", err)
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open task repository %s: %w", repoPath, err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	fullPath := filepath.Join(repoPath, spec.Path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read task file %s: %w", fullPath, err)
	}

	currentSpec, err := taskcontract.ParseTaskDocument(content)
	if err != nil {
		return fmt.Errorf("parse current task document %s: %w", fullPath, err)
	}

	// 1. Idempotency check: if current file in Git already records this terminal result
	if currentSpec.Result != nil && currentSpec.Result.IsTerminal() {
		if currentSpec.Result.Status == result.Status &&
			(result.Revision == "" || currentSpec.Result.Revision == result.Revision) {
			log.Printf("taskd: task %s at %s already published with matching result %s, skipping",
				spec.ID, spec.Path, result.Status)
			return nil
		}
		return fmt.Errorf("conflict: task %s at %s already has terminal result %s (attempted to publish %s)",
			spec.ID, spec.Path, currentSpec.Result.Status, result.Status)
	}

	// 2. Conflict check: if task specification body was modified since spec was constructed
	if strings.TrimSpace(currentSpec.Requirements) != strings.TrimSpace(spec.Requirements) ||
		strings.TrimSpace(currentSpec.Context) != strings.TrimSpace(spec.Context) ||
		strings.TrimSpace(currentSpec.AcceptanceCriteria) != strings.TrimSpace(spec.AcceptanceCriteria) {
		return fmt.Errorf("conflict: task %s at %s was concurrently modified with new requirements; refusing to overwrite with stale result",
			spec.ID, spec.Path)
	}

	// 3. Format document with terminal result
	updatedBytes, err := taskcontract.FormatTaskWithResult(content, result)
	if err != nil {
		return fmt.Errorf("format task with result: %w", err)
	}

	if err := os.WriteFile(fullPath, updatedBytes, 0644); err != nil {
		return fmt.Errorf("write updated task file %s: %w", fullPath, err)
	}

	// 4. Stage and commit
	if _, err := worktree.Add(spec.Path); err != nil {
		return fmt.Errorf("stage %s: %w", spec.Path, err)
	}

	commitMsg := fmt.Sprintf("taskd: record terminal result for %s (%s)", spec.ID, result.Status)
	commitHash, err := worktree.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Lattice taskd",
			Email: "taskd@lattice.local",
			When:  time.Now(),
		},
		Committer: &object.Signature{
			Name:  "Lattice taskd",
			Email: "taskd@lattice.local",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("commit result to Git: %w", err)
	}

	// Ensure branch ref points to new commit
	headRef, err := repo.Head()
	if err == nil && headRef.Name().IsBranch() {
		_ = repo.Storer.SetReference(plumbing.NewHashReference(headRef.Name(), commitHash))
	}

	log.Printf("taskd: published terminal result for %s to Git (%s, commit %s)",
		spec.ID, result.Status, commitHash.String())
	return nil
}
