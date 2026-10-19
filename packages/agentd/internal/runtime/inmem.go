package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// InMemRuntime provides an in-memory implementation for hermetic testing.
type InMemRuntime struct{}

// NewInMemRuntime creates a new InMemRuntime.
func NewInMemRuntime() *InMemRuntime {
	return &InMemRuntime{}
}

// InMemInstance represents an in-memory workload instance.
type InMemInstance struct {
	endpoint string
	commit   string
	closed   bool
}

func (i *InMemInstance) Endpoint() string { return i.endpoint }
func (i *InMemInstance) WorkDir() string  { return "" }
func (i *InMemInstance) Commit() string   { return i.commit }
func (i *InMemInstance) SetCommit(h string) { i.commit = h }
func (i *InMemInstance) Stop(_ context.Context) error {
	i.closed = true
	return nil
}

// Start creates an in-memory git commit and returns an InMemInstance.
func (r *InMemRuntime) Start(_ context.Context, spec *taskcontract.TaskSpec) (Instance, error) {
	if spec == nil {
		return nil, errors.New("cannot start with nil TaskSpec")
	}

	storer := memory.NewStorage()
	fs := memfs.New()
	repo, err := git.Init(storer, fs)
	if err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("git worktree: %w", err)
	}

	f, err := fs.Create("README.md")
	if err != nil {
		return nil, err
	}
	f.Write([]byte(fmt.Sprintf("# Task %s\n", spec.ID)))
	f.Close()

	if _, err := wt.Add("README.md"); err != nil {
		return nil, err
	}

	commitHash, err := wt.Commit(fmt.Sprintf("init task %s", spec.ID), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "InMem Agent",
			Email: "agent@lattice.local",
			When:  time.Unix(0, 0),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	return &InMemInstance{
		endpoint: fmt.Sprintf("inmem://%s", spec.ID),
		commit:   commitHash.String(),
	}, nil
}
