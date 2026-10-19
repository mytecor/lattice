package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mytecor/lattice/agentd/internal/acp"
	"github.com/mytecor/lattice/agentd/internal/runtime"
	"github.com/mytecor/lattice/agentd/internal/verifier"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// mockRuntime provides custom Instance or error.
type mockRuntime struct {
	startErr error
	inst     runtime.Instance
}

func (m *mockRuntime) Start(_ context.Context, spec *taskcontract.TaskSpec) (runtime.Instance, error) {
	if m.startErr != nil {
		return nil, m.startErr
	}
	return m.inst, nil
}

type mockInstance struct {
	endpoint string
	workDir  string
	commit   string
	stopped  bool
}

func (m *mockInstance) Endpoint() string   { return m.endpoint }
func (m *mockInstance) WorkDir() string    { return m.workDir }
func (m *mockInstance) Commit() string     { return m.commit }
func (m *mockInstance) SetCommit(h string) { m.commit = h }
func (m *mockInstance) Stop(_ context.Context) error {
	m.stopped = true
	return nil
}

// mockVerifier provides scripted verifier responses.
type mockVerifier struct {
	verifyFunc func(ctx context.Context, spec *taskcontract.TaskSpec, workDir string) (*verifier.Result, error)
}

func (m *mockVerifier) Verify(ctx context.Context, spec *taskcontract.TaskSpec, workDir string) (*verifier.Result, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, spec, workDir)
	}
	return &verifier.Result{Passed: true, Summary: "mock passed"}, nil
}

func TestExecutionLoop_SingleTurnSuccess(t *testing.T) {
	inst := &mockInstance{
		endpoint: "mock://inst-1",
		workDir:  "/tmp/work-1",
		commit:   "commit-123",
	}
	rt := &mockRuntime{inst: inst}
	acpMock := acp.NewMockClient()
	ver := &mockVerifier{
		verifyFunc: func(ctx context.Context, spec *taskcontract.TaskSpec, workDir string) (*verifier.Result, error) {
			return &verifier.Result{Passed: true, Summary: "all checks passed"}, nil
		},
	}

	execLoop, err := New(Config{
		Runtime:   rt,
		ACPClient: acpMock,
		Verifier:  ver,
	})
	if err != nil {
		t.Fatalf("New loop failed: %v", err)
	}

	spec := &taskcontract.TaskSpec{
		ID:           "task-loop-01",
		Source:       taskcontract.SourceSpec{Repo: "https://example.com/repo.git"},
		Requirements: "Write code",
	}

	res, err := execLoop.ExecuteTask(context.Background(), spec)
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}
	if res.Status != taskcontract.StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s", res.Status)
	}
	if res.Revision != "commit-123" {
		t.Errorf("expected revision commit-123, got %s", res.Revision)
	}
	if !inst.stopped {
		t.Error("expected instance to be stopped after execution")
	}
}

func TestExecutionLoop_MultiTurnFeedback(t *testing.T) {
	inst := &mockInstance{
		endpoint: "mock://inst-2",
		workDir:  "/tmp/work-2",
		commit:   "commit-456",
	}
	rt := &mockRuntime{inst: inst}

	var promptsReceived []string
	acpMock := acp.NewMockClient()
	acpMock.TurnHandler = func(ctx context.Context, prompt string, turnIndex int) (*acp.TurnResult, error) {
		promptsReceived = append(promptsReceived, prompt)
		if turnIndex == 0 {
			return &acp.TurnResult{Status: acp.TurnCompleted, Message: "first attempt done"}, nil
		}
		return &acp.TurnResult{Status: acp.TurnCompleted, Message: "fixed code based on feedback"}, nil
	}

	verifyCount := 0
	ver := &mockVerifier{
		verifyFunc: func(ctx context.Context, spec *taskcontract.TaskSpec, workDir string) (*verifier.Result, error) {
			verifyCount++
			if verifyCount == 1 {
				return &verifier.Result{
					Passed:  false,
					Summary: "1 check failed",
					Output:  "FAIL: TestFeature",
				}, nil
			}
			return &verifier.Result{
				Passed:  true,
				Summary: "All tests pass",
			}, nil
		},
	}

	execLoop, err := New(Config{
		Runtime:              rt,
		ACPClient:            acpMock,
		Verifier:             ver,
		DefaultMaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("New loop failed: %v", err)
	}

	spec := &taskcontract.TaskSpec{
		ID:           "task-loop-02",
		Source:       taskcontract.SourceSpec{Repo: "https://example.com/repo.git"},
		Requirements: "Implement feature",
	}

	res, err := execLoop.ExecuteTask(context.Background(), spec)
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}
	if res.Status != taskcontract.StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s: %+v", res.Status, res)
	}
	if verifyCount != 2 {
		t.Errorf("expected 2 verifications, got %d", verifyCount)
	}
	if len(promptsReceived) != 2 {
		t.Fatalf("expected 2 prompts sent to agent, got %d", len(promptsReceived))
	}
	if !strings.Contains(promptsReceived[1], "FAIL: TestFeature") {
		t.Errorf("expected 2nd prompt to contain feedback, got: %s", promptsReceived[1])
	}
}

func TestExecutionLoop_ExhaustedIterations(t *testing.T) {
	inst := &mockInstance{
		endpoint: "mock://inst-3",
		workDir:  "/tmp/work-3",
		commit:   "commit-789",
	}
	rt := &mockRuntime{inst: inst}
	acpMock := acp.NewMockClient()

	verifyCount := 0
	ver := &mockVerifier{
		verifyFunc: func(ctx context.Context, spec *taskcontract.TaskSpec, workDir string) (*verifier.Result, error) {
			verifyCount++
			return &verifier.Result{
				Passed:  false,
				Summary: "check persistently failing",
				Output:  "error: syntax error",
			}, nil
		},
	}

	execLoop, _ := New(Config{
		Runtime:              rt,
		ACPClient:            acpMock,
		Verifier:             ver,
		DefaultMaxIterations: 2,
	})

	spec := &taskcontract.TaskSpec{
		ID:     "task-loop-03",
		Source: taskcontract.SourceSpec{Repo: "https://example.com/repo.git"},
	}

	res, err := execLoop.ExecuteTask(context.Background(), spec)
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}
	if res.Status != taskcontract.StatusFailed {
		t.Fatalf("expected StatusFailed when iterations exhausted, got %s", res.Status)
	}
	if verifyCount != 2 {
		t.Errorf("expected exactly 2 verification attempts, got %d", verifyCount)
	}
}

func TestExecutionLoop_ConflictBlocked(t *testing.T) {
	rt := &mockRuntime{
		startErr: runtime.ErrConflict,
	}
	acpMock := acp.NewMockClient()
	ver := &mockVerifier{}

	execLoop, _ := New(Config{
		Runtime:   rt,
		ACPClient: acpMock,
		Verifier:  ver,
	})

	spec := &taskcontract.TaskSpec{
		ID:     "task-loop-04",
		Source: taskcontract.SourceSpec{Repo: "https://example.com/repo.git"},
	}

	res, err := execLoop.ExecuteTask(context.Background(), spec)
	if err != nil {
		t.Fatalf("ExecuteTask error: %v", err)
	}
	if res.Status != taskcontract.StatusBlocked {
		t.Fatalf("expected StatusBlocked on conflict, got %s", res.Status)
	}
}

func TestExecutionLoop_ContextTimeout(t *testing.T) {
	inst := &mockInstance{
		endpoint: "mock://inst-5",
		workDir:  "/tmp/work-5",
	}
	rt := &mockRuntime{inst: inst}
	acpMock := acp.NewMockClient()
	acpMock.TurnHandler = func(ctx context.Context, prompt string, turnIndex int) (*acp.TurnResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ver := &mockVerifier{}

	execLoop, _ := New(Config{
		Runtime:   rt,
		ACPClient: acpMock,
		Verifier:  ver,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	spec := &taskcontract.TaskSpec{
		ID:     "task-loop-05",
		Source: taskcontract.SourceSpec{Repo: "https://example.com/repo.git"},
	}

	res, err := execLoop.ExecuteTask(ctx, spec)
	// Either error is ctx.Err() or TaskResult.Status is StatusFailed
	if err == nil && res.Status != taskcontract.StatusFailed {
		t.Fatalf("expected failure or context error on timeout, got res=%+v", res)
	}
}
