package main

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	taskcontract "github.com/mytecor/lattice/task-contract"
	"github.com/mytecor/meshbus/core"
)

type mockSubscription struct {
	onClose func()
}

func (s *mockSubscription) Close() error {
	if s.onClose != nil {
		s.onClose()
	}
	return nil
}

type mockBus struct {
	mu          sync.Mutex
	subscribers map[string][]func(context.Context, core.ReceivedEvent) error
}

func newMockBus() *mockBus {
	return &mockBus{
		subscribers: make(map[string][]func(context.Context, core.ReceivedEvent) error),
	}
}

func (b *mockBus) Subscribe(pattern string, handler func(context.Context, core.ReceivedEvent) error) (io.Closer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[pattern] = append(b.subscribers[pattern], handler)
	return &mockSubscription{
		onClose: func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subscribers, pattern)
		},
	}, nil
}

func (b *mockBus) Publish(ctx context.Context, topic string, payload []byte, opts core.PublishOptions) error {
	b.mu.Lock()
	handlers := append([]func(context.Context, core.ReceivedEvent) error(nil), b.subscribers[topic]...)
	b.mu.Unlock()

	for _, h := range handlers {
		go func(fn func(context.Context, core.ReceivedEvent) error) {
			_ = fn(ctx, core.ReceivedEvent{
				Event: core.Event{
					Topic:   topic,
					Payload: payload,
				},
			})
		}(h)
	}
	return nil
}

func TestServerRequestResponse(t *testing.T) {
	bus := newMockBus()
	executor := &InMemDummyWorkloadExecutor{}

	server, err := NewServer(ServerConfig{
		Bus:          bus,
		AgentSubject: "agent.run",
		Executor:     executor,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = server.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	replyCh := make(chan *taskcontract.RunTaskResponse, 1)
	_, err = bus.Subscribe("reply.test", func(_ context.Context, event core.ReceivedEvent) error {
		var resp taskcontract.RunTaskResponse
		if err := json.Unmarshal(event.Payload, &resp); err == nil {
			replyCh <- &resp
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe to reply: %v", err)
	}

	req := taskcontract.RunTaskRequest{
		ID: "req-1",
		Spec: taskcontract.TaskSpec{
			ID:       "task-rpc-01",
			TaskRepo: "/var/lib/tasks",
			Path:     "tasks/task-01.md",
			Commit:   "c1",
			Source: taskcontract.SourceSpec{
				Repo:     "/tmp/source",
				Revision: "main",
			},
		},
		ReplyTo: "reply.test",
	}

	reqBytes, _ := json.Marshal(req)
	err = bus.Publish(ctx, "agent.run", reqBytes, core.PublishOptions{})
	if err != nil {
		t.Fatalf("failed to publish request: %v", err)
	}

	select {
	case resp := <-replyCh:
		if resp.ID != "req-1" {
			t.Errorf("expected ID req-1, got %s", resp.ID)
		}
		if resp.Result == nil {
			t.Fatalf("expected non-nil result")
		}
		if resp.Result.Status != taskcontract.StatusCompleted {
			t.Errorf("expected status completed, got %s", resp.Result.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for agentd response")
	}
}
