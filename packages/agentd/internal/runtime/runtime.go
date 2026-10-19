package runtime

import (
	"context"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

// Instance represents a prepared or running workload environment.
type Instance interface {
	// Endpoint returns the ACP endpoint address for client communication.
	Endpoint() string
	// WorkDir returns the local workspace/worktree path, if any.
	WorkDir() string
	// Commit returns the resulting git commit SHA produced by the workload, if any.
	Commit() string
	// SetCommit updates the recorded commit SHA.
	SetCommit(hash string)
	// Stop releases ephemeral resources and stops the workload.
	Stop(ctx context.Context) error
}

// Runtime prepares and starts execution environments for task specifications.
type Runtime interface {
	Start(ctx context.Context, spec *taskcontract.TaskSpec) (Instance, error)
}
