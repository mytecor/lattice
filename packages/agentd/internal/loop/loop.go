package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mytecor/lattice/agentd/internal/acp"
	"github.com/mytecor/lattice/agentd/internal/runtime"
	"github.com/mytecor/lattice/agentd/internal/verifier"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// DefaultMaxIterations is the default bound on agent execution loop turns.
const DefaultMaxIterations = 5

// Config configures the ExecutionLoop.
type Config struct {
	Runtime              runtime.Runtime
	ACPClient            acp.Client
	Verifier             verifier.Verifier
	DefaultMaxIterations int
}

// ExecutionLoop orchestrates the single-agent execution loop:
// starts workload via Runtime, interacts via ACP, and runs bounded verification.
type ExecutionLoop struct {
	runtime              runtime.Runtime
	acpClient            acp.Client
	verifier             verifier.Verifier
	defaultMaxIterations int
}

// New creates a new ExecutionLoop from configuration.
func New(cfg Config) (*ExecutionLoop, error) {
	if cfg.Runtime == nil {
		return nil, errors.New("runtime is required")
	}
	if cfg.ACPClient == nil {
		return nil, errors.New("acpClient is required")
	}
	if cfg.Verifier == nil {
		return nil, errors.New("verifier is required")
	}
	maxIter := cfg.DefaultMaxIterations
	if maxIter <= 0 {
		maxIter = DefaultMaxIterations
	}

	return &ExecutionLoop{
		runtime:              cfg.Runtime,
		acpClient:            cfg.ACPClient,
		verifier:             cfg.Verifier,
		defaultMaxIterations: maxIter,
	}, nil
}

// ExecuteTask executes a TaskSpec through the bounded execution loop.
func (l *ExecutionLoop) ExecuteTask(ctx context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	if spec == nil {
		return nil, errors.New("cannot execute nil TaskSpec")
	}
	if spec.Source.Repo == "" {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			ResultText:       "Source repository not specified in TaskSpec",
			VerificationText: "Validation failed: spec.source.repo is required",
		}, nil
	}

	// 1. Start workload instance via Runtime
	inst, err := l.runtime.Start(ctx, spec)
	if err != nil {
		if errors.Is(err, runtime.ErrConflict) || strings.Contains(err.Error(), "conflict") {
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusBlocked,
				ResultText:       fmt.Sprintf("Conflict detected: %v", err),
				VerificationText: "Existing commit contains incompatible task payload",
			}, nil
		}
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			ResultText:       fmt.Sprintf("Runtime failed to start workload: %v", err),
			VerificationText: "Workload startup failed",
		}, nil
	}
	defer func() {
		_ = inst.Stop(context.Background())
	}()

	// If WorkDir is empty, instance indicates an existing idempotent commit replay
	if inst.WorkDir() == "" && inst.Commit() != "" {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusCompleted,
			Revision:         inst.Commit(),
			ResultText:       fmt.Sprintf("Idempotent replay: deterministic commit %s already exists", inst.Commit()),
			VerificationText: "Verified existing equivalent commit in source repository",
		}, nil
	}

	// 2. Connect ACP session to workload
	session, err := l.acpClient.Connect(ctx, inst.Endpoint())
	if err != nil {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			Revision:         inst.Commit(),
			ResultText:       fmt.Sprintf("Failed to connect ACP session: %v", err),
			VerificationText: "ACP endpoint unreachable",
		}, nil
	}
	defer session.Close()

	// 3. Initialize ACP session with TaskSpec
	if err := session.Initialize(ctx, spec); err != nil {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			Revision:         inst.Commit(),
			ResultText:       fmt.Sprintf("Failed to initialize ACP session: %v", err),
			VerificationText: "ACP session initialization failed",
		}, nil
	}

	// 4. Bounded execution & verification loop
	maxIterations := l.defaultMaxIterations
	if spec.Runtime.MaxIterations > 0 {
		maxIterations = spec.Runtime.MaxIterations
	}

	currentPrompt := buildInitialPrompt(spec)

	for iter := 1; iter <= maxIterations; iter++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		turnRes, err := session.Prompt(ctx, currentPrompt)
		if err != nil {
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusFailed,
				Revision:         inst.Commit(),
				ResultText:       fmt.Sprintf("ACP turn failed: %v", err),
				VerificationText: "Agent turn error",
			}, nil
		}
		if turnRes != nil && turnRes.Status == acp.TurnFailed {
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusFailed,
				Revision:         inst.Commit(),
				ResultText:       fmt.Sprintf("Agent reported failure on iteration %d: %s", iter, turnRes.Message),
				VerificationText: "Agent execution failed",
			}, nil
		}

		// Run verifier checks
		verRes, err := l.verifier.Verify(ctx, spec, inst.WorkDir())
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusFailed,
				Revision:         inst.Commit(),
				ResultText:       fmt.Sprintf("Verification execution error: %v", err),
				VerificationText: "Verifier execution error",
			}, nil
		}

		if verRes.Passed {
			// All checks passed!
			agentMsg := ""
			if turnRes != nil && turnRes.Message != "" {
				agentMsg = fmt.Sprintf(": %s", turnRes.Message)
			}
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusCompleted,
				Revision:         inst.Commit(),
				ResultText:       fmt.Sprintf("Task %s completed in %d iteration(s)%s", spec.ID, iter, agentMsg),
				VerificationText: verRes.Summary,
			}, nil
		}

		// Check if iterations exhausted
		if iter >= maxIterations {
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusFailed,
				Revision:         inst.Commit(),
				ResultText:       fmt.Sprintf("Verification failed after %d iteration(s): %s", maxIterations, verRes.Summary),
				VerificationText: verRes.Output,
			}, nil
		}

		// Provide verification failure details as feedback for the next turn
		currentPrompt = fmt.Sprintf("Verification check(s) failed on iteration %d:\n%s\nPlease fix the issues and finish the task.", iter, verRes.Output)
	}

	return &taskcontract.TaskResult{
		Status:           taskcontract.StatusFailed,
		Revision:         inst.Commit(),
		ResultText:       fmt.Sprintf("Loop terminated after %d iterations without passing verification", maxIterations),
		VerificationText: "Iteration limit reached",
	}, nil
}

func buildInitialPrompt(spec *taskcontract.TaskSpec) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Task ID: %s\n", spec.ID))
	if spec.Title != "" {
		b.WriteString(fmt.Sprintf("Title: %s\n", spec.Title))
	}
	if spec.Context != "" {
		b.WriteString(fmt.Sprintf("\nContext:\n%s\n", spec.Context))
	}
	if spec.Requirements != "" {
		b.WriteString(fmt.Sprintf("\nRequirements:\n%s\n", spec.Requirements))
	}
	if spec.AcceptanceCriteria != "" {
		b.WriteString(fmt.Sprintf("\nAcceptance Criteria:\n%s\n", spec.AcceptanceCriteria))
	}
	return b.String()
}
