package acp

import (
	"context"
	"errors"
	"fmt"
	"sync"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

// DummyClient provides a deterministic default ACP client.
type DummyClient struct{}

// NewDummyClient creates a new DummyClient.
func NewDummyClient() *DummyClient {
	return &DummyClient{}
}

// Connect returns a dummy session.
func (c *DummyClient) Connect(_ context.Context, endpoint string) (Session, error) {
	if endpoint == "" {
		return nil, errors.New("empty ACP endpoint")
	}
	return &dummySession{endpoint: endpoint}, nil
}

type dummySession struct {
	endpoint string
	closed   bool
}

func (s *dummySession) Initialize(_ context.Context, spec *taskcontract.TaskSpec) error {
	if s.closed {
		return errors.New("session already closed")
	}
	if spec == nil {
		return errors.New("cannot initialize with nil spec")
	}
	return nil
}

func (s *dummySession) Prompt(_ context.Context, prompt string) (*TurnResult, error) {
	if s.closed {
		return nil, errors.New("session already closed")
	}
	return &TurnResult{
		Status:  TurnCompleted,
		Message: fmt.Sprintf("dummy agent processed turn (prompt len %d)", len(prompt)),
	}, nil
}

func (s *dummySession) Close() error {
	s.closed = true
	return nil
}

// MockTurnHandler computes TurnResult from prompt and turn index.
type MockTurnHandler func(ctx context.Context, prompt string, turnIndex int) (*TurnResult, error)

// MockClient allows full control over ACP turns and connection behaviour in contract tests.
type MockClient struct {
	mu           sync.Mutex
	ConnectErr   error
	TurnHandler  MockTurnHandler
	Sessions     []*MockSession
	ConnectCount int
}

// NewMockClient creates a new MockClient.
func NewMockClient() *MockClient {
	return &MockClient{}
}

// Connect creates a new MockSession.
func (m *MockClient) Connect(_ context.Context, endpoint string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConnectCount++
	if m.ConnectErr != nil {
		return nil, m.ConnectErr
	}

	sess := &MockSession{
		Endpoint:    endpoint,
		turnHandler: m.TurnHandler,
	}
	m.Sessions = append(m.Sessions, sess)
	return sess, nil
}

// MockSession tracks ACP session calls.
type MockSession struct {
	mu          sync.Mutex
	Endpoint    string
	Spec        *taskcontract.TaskSpec
	Prompts     []string
	Closed      bool
	turnHandler MockTurnHandler
}

// Initialize records the spec.
func (s *MockSession) Initialize(_ context.Context, spec *taskcontract.TaskSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Closed {
		return errors.New("session closed")
	}
	s.Spec = spec
	return nil
}

// Prompt delegates to TurnHandler or returns TurnCompleted.
func (s *MockSession) Prompt(ctx context.Context, prompt string) (*TurnResult, error) {
	s.mu.Lock()
	if s.Closed {
		s.mu.Unlock()
		return nil, errors.New("session closed")
	}
	turnIdx := len(s.Prompts)
	s.Prompts = append(s.Prompts, prompt)
	handler := s.turnHandler
	s.mu.Unlock()

	if handler != nil {
		return handler(ctx, prompt, turnIdx)
	}

	return &TurnResult{
		Status:  TurnCompleted,
		Message: fmt.Sprintf("turn %d finished", turnIdx),
	}, nil
}

// Close marks session closed.
func (s *MockSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Closed = true
	return nil
}
