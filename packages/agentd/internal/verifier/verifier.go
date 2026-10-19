package verifier

import (
	"context"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

// Result represents the outcome of running verification checks.
type Result struct {
	Passed       bool     `json:"passed"`
	Summary      string   `json:"summary"`
	Output       string   `json:"output,omitempty"`
	FailedChecks []string `json:"failed_checks,omitempty"`
}

// Verifier runs verification checks against a workload workspace.
type Verifier interface {
	Verify(ctx context.Context, spec *taskcontract.TaskSpec, workDir string) (*Result, error)
}
