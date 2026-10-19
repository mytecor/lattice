package acp

import (
	"context"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

// TurnStatus represents the outcome of an agent turn.
type TurnStatus string

const (
	TurnCompleted TurnStatus = "completed"
	TurnFailed    TurnStatus = "failed"
)

// TurnResult carries the response of an agent turn.
type TurnResult struct {
	Status  TurnStatus `json:"status"`
	Message string     `json:"message,omitempty"`
}

// Client connects to an ACP service inside an agent workload.
type Client interface {
	Connect(ctx context.Context, endpoint string) (Session, error)
}

// Session represents an established ACP session with an agent workload.
type Session interface {
	// Initialize sends the task specification and establishes agent context.
	Initialize(ctx context.Context, spec *taskcontract.TaskSpec) error
	// Prompt sends instructions or feedback to the agent and awaits the turn result.
	Prompt(ctx context.Context, prompt string) (*TurnResult, error)
	// Close releases the session.
	Close() error
}
