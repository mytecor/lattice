package acp

import (
	"context"
	"errors"
	"testing"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

func TestDummyClient_Lifecycle(t *testing.T) {
	client := NewDummyClient()
	ctx := context.Background()

	sess, err := client.Connect(ctx, "acp://127.0.0.1:9000")
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	spec := &taskcontract.TaskSpec{ID: "task-acp-01"}
	if err := sess.Initialize(ctx, spec); err != nil {
		t.Fatalf("failed to initialize session: %v", err)
	}

	res, err := sess.Prompt(ctx, "Please implement feature X")
	if err != nil {
		t.Fatalf("failed to prompt: %v", err)
	}
	if res.Status != TurnCompleted {
		t.Fatalf("expected TurnCompleted, got %s", res.Status)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("failed to close session: %v", err)
	}

	// Post-close prompt should fail
	if _, err := sess.Prompt(ctx, "After close"); err == nil {
		t.Fatal("expected error after close, got nil")
	}
}

func TestMockClient_MultiTurnWithHandler(t *testing.T) {
	mock := NewMockClient()
	mock.TurnHandler = func(ctx context.Context, prompt string, turnIndex int) (*TurnResult, error) {
		if turnIndex == 0 {
			return &TurnResult{Status: TurnCompleted, Message: "first attempt finished"}, nil
		}
		return &TurnResult{Status: TurnCompleted, Message: "fixed after feedback: " + prompt}, nil
	}

	ctx := context.Background()
	sess, err := mock.Connect(ctx, "mock://session-1")
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	spec := &taskcontract.TaskSpec{ID: "task-mock-01"}
	_ = sess.Initialize(ctx, spec)

	res1, err := sess.Prompt(ctx, "initial instruction")
	if err != nil || res1.Message != "first attempt finished" {
		t.Fatalf("turn 0 unexpected: res=%+v, err=%v", res1, err)
	}

	res2, err := sess.Prompt(ctx, "check failed on test 2")
	if err != nil || res2.Message != "fixed after feedback: check failed on test 2" {
		t.Fatalf("turn 1 unexpected: res=%+v, err=%v", res2, err)
	}

	_ = sess.Close()
	if len(mock.Sessions) != 1 {
		t.Fatalf("expected 1 recorded session, got %d", len(mock.Sessions))
	}
	recorded := mock.Sessions[0]
	if len(recorded.Prompts) != 2 {
		t.Fatalf("expected 2 prompts recorded, got %d", len(recorded.Prompts))
	}
}

func TestMockClient_ConnectionError(t *testing.T) {
	mock := NewMockClient()
	mock.ConnectErr = errors.New("connection refused")

	_, err := mock.Connect(context.Background(), "mock://unavailable")
	if err == nil || err.Error() != "connection refused" {
		t.Fatalf("expected connection error, got %v", err)
	}
}
