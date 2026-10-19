package main

import (
	"context"
	"sync"

	"github.com/mytecor/lattice/agentd/internal/acp"
	"github.com/mytecor/lattice/agentd/internal/loop"
	"github.com/mytecor/lattice/agentd/internal/runtime"
	"github.com/mytecor/lattice/agentd/internal/verifier"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// Executor executes a TaskSpec and returns the terminal TaskResult.
// It does NOT retain durable state across invocations or restarts.
type Executor interface {
	ExecuteTask(ctx context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error)
}

// DummyWorkloadData represents the deterministic payload written by the dummy workload.
type DummyWorkloadData = runtime.DummyWorkloadData

// DummyWorkloadExecutor runs a deterministic workload against a source Git repository
// using the decomposed components: runtime.DummyRuntime, acp.DummyClient, and verifier.CommandVerifier in loop.ExecutionLoop.
type DummyWorkloadExecutor struct {
	workDir string
	mu      sync.Mutex
	loop    *loop.ExecutionLoop
}

// NewDummyWorkloadExecutor creates a new DummyWorkloadExecutor backed by ExecutionLoop.
func NewDummyWorkloadExecutor(workDir string) *DummyWorkloadExecutor {
	rt := runtime.NewDummyRuntime(workDir)
	acpClient := acp.NewDummyClient()
	ver := verifier.NewCommandVerifier()
	execLoop, _ := loop.New(loop.Config{
		Runtime:   rt,
		ACPClient: acpClient,
		Verifier:  ver,
	})
	return &DummyWorkloadExecutor{
		workDir: workDir,
		loop:    execLoop,
	}
}

// ExecuteTask runs the task via the execution loop.
func (e *DummyWorkloadExecutor) ExecuteTask(ctx context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	e.mu.Lock()
	if e.loop == nil {
		rt := runtime.NewDummyRuntime(e.workDir)
		acpClient := acp.NewDummyClient()
		ver := verifier.NewCommandVerifier()
		e.loop, _ = loop.New(loop.Config{
			Runtime:   rt,
			ACPClient: acpClient,
			Verifier:  ver,
		})
	}
	execLoop := e.loop
	e.mu.Unlock()

	return execLoop.ExecuteTask(ctx, spec)
}

// InMemDummyWorkloadExecutor performs deterministic dummy execution purely in-memory.
type InMemDummyWorkloadExecutor struct {
	mu   sync.Mutex
	loop *loop.ExecutionLoop
}

// NewInMemDummyWorkloadExecutor creates an in-memory executor.
func NewInMemDummyWorkloadExecutor() *InMemDummyWorkloadExecutor {
	rt := runtime.NewInMemRuntime()
	acpClient := acp.NewDummyClient()
	ver := verifier.NewCommandVerifier()
	execLoop, _ := loop.New(loop.Config{
		Runtime:   rt,
		ACPClient: acpClient,
		Verifier:  ver,
	})
	return &InMemDummyWorkloadExecutor{
		loop: execLoop,
	}
}

// ExecuteTask runs the in-memory task via the execution loop.
func (e *InMemDummyWorkloadExecutor) ExecuteTask(ctx context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	e.mu.Lock()
	if e.loop == nil {
		rt := runtime.NewInMemRuntime()
		acpClient := acp.NewDummyClient()
		ver := verifier.NewCommandVerifier()
		e.loop, _ = loop.New(loop.Config{
			Runtime:   rt,
			ACPClient: acpClient,
			Verifier:  ver,
		})
	}
	execLoop := e.loop
	e.mu.Unlock()

	return execLoop.ExecuteTask(ctx, spec)
}
