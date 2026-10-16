package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	taskcontract "github.com/mytecor/lattice/task-contract"
	"github.com/mytecor/meshbus/core"
)

// Bus abstracts the Meshbus operations used by agentd.
type Bus interface {
	Subscribe(pattern string, handler func(ctx context.Context, event core.ReceivedEvent) error) (io.Closer, error)
	Publish(ctx context.Context, topic string, payload []byte, opts core.PublishOptions) error
}

// ServerConfig configures the agentd server.
type ServerConfig struct {
	Bus          Bus
	AgentSubject string
	Executor     Executor
}

// Server is the agentd daemon server.
// It subscribes to the agent execution subject on Meshbus, invokes Executor
// for incoming task requests, and returns terminal TaskResult responses.
type Server struct {
	bus          Bus
	agentSubject string
	executor     Executor

	mu         sync.Mutex
	activeRuns map[string]context.CancelFunc // reqID -> cancel
	wg         sync.WaitGroup
}

// NewServer creates a new agentd Server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Bus == nil {
		return nil, errors.New("bus is required")
	}
	if cfg.AgentSubject == "" {
		cfg.AgentSubject = taskcontract.DefaultSubjectAgentRun
	}
	if cfg.Executor == nil {
		return nil, errors.New("executor is required")
	}

	return &Server{
		bus:          cfg.Bus,
		agentSubject: cfg.AgentSubject,
		executor:     cfg.Executor,
		activeRuns:   make(map[string]context.CancelFunc),
	}, nil
}

// handleRequest processes one incoming RunTaskRequest.
func (s *Server) handleRequest(parentCtx context.Context, event core.ReceivedEvent) error {
	var req taskcontract.RunTaskRequest
	if err := json.Unmarshal(event.Payload, &req); err != nil {
		log.Printf("agentd: invalid request payload: %v", err)
		return nil
	}

	if req.ReplyTo == "" {
		log.Printf("agentd: request %s has no reply_to, ignoring", req.ID)
		return nil
	}

	log.Printf("agentd: received task execution request %s for task %s (rev %s)",
		req.ID, req.Spec.ID, req.Spec.RevisionKey())

	taskTimeout := 10 * time.Minute
	if req.Spec.Runtime.Timeout != "" {
		if d, err := time.ParseDuration(req.Spec.Runtime.Timeout); err == nil && d > 0 {
			taskTimeout = d
		}
	}

	runCtx, cancel := context.WithTimeout(parentCtx, taskTimeout)

	s.mu.Lock()
	s.activeRuns[req.ID] = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.activeRuns, req.ID)
			s.mu.Unlock()
		}()

		result, err := s.executor.ExecuteTask(runCtx, &req.Spec)
		var resp taskcontract.RunTaskResponse
		resp.ID = req.ID

		if err != nil {
			log.Printf("agentd: task %s execution failed with error: %v", req.Spec.ID, err)
			resp.Error = err.Error()
		} else {
			resp.Result = result
			if result != nil {
				log.Printf("agentd: task %s execution completed with status %s",
					req.Spec.ID, result.Status)
			}
		}

		respBytes, mErr := json.Marshal(resp)
		if mErr != nil {
			log.Printf("agentd: failed to marshal response for %s: %v", req.ID, mErr)
			return
		}

		if pubErr := s.bus.Publish(parentCtx, req.ReplyTo, respBytes, core.PublishOptions{
			TTL:         5 * time.Minute,
			ContentType: "application/json",
		}); pubErr != nil {
			log.Printf("agentd: failed to publish response to %s: %v", req.ReplyTo, pubErr)
		}
	}()

	return nil
}

// Run starts the agentd server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	sub, err := s.bus.Subscribe(s.agentSubject, func(subCtx context.Context, event core.ReceivedEvent) error {
		return s.handleRequest(subCtx, event)
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", s.agentSubject, err)
	}
	defer sub.Close()

	log.Printf("agentd: listening on subject %s", s.agentSubject)

	<-ctx.Done()
	s.Stop()
	return ctx.Err()
}

// Stop cancels active tasks and waits for them to terminate.
func (s *Server) Stop() {
	s.mu.Lock()
	for _, cancel := range s.activeRuns {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
