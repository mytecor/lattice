package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mytecor/meshbus/core"
)

// Bus abstracts publish/subscribe operations (satisfied by *core.Node).
type Bus interface {
	Publish(ctx context.Context, topic string, payload []byte, options core.PublishOptions) (core.PublishResult, error)
	Subscribe(pattern string, handler core.EventHandler) (*core.Subscription, error)
}

// ServerConfig configures the git-watchd RPC server.
type ServerConfig struct {
	Bus          Bus
	Engine       GitEngine
	Watcher      *Watcher
	HeadSubject  string
	WatchSubject string
}

// Server exposes git.head and git.watch RPC handlers on Meshbus.
type Server struct {
	bus          Bus
	engine       GitEngine
	watcher      *Watcher
	headSubject  string
	watchSubject string

	mu            sync.Mutex
	subscriptions []*core.Subscription
	closed        bool
}

// NewServer creates a new Server instance.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Bus == nil {
		return nil, errors.New("bus is required")
	}
	if cfg.Engine == nil {
		return nil, errors.New("engine is required")
	}
	if cfg.Watcher == nil {
		cfg.Watcher = NewWatcher(cfg.Engine, DefaultPollInterval)
	}
	if cfg.HeadSubject == "" {
		cfg.HeadSubject = DefaultSubjectHead
	}
	if cfg.WatchSubject == "" {
		cfg.WatchSubject = DefaultSubjectWatch
	}

	return &Server{
		bus:          cfg.Bus,
		engine:       cfg.Engine,
		watcher:      cfg.Watcher,
		headSubject:  cfg.HeadSubject,
		watchSubject: cfg.WatchSubject,
	}, nil
}

// Start registers subscriptions for git.head and git.watch.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("server is closed")
	}

	headSub, err := s.bus.Subscribe(s.headSubject, s.handleHead)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", s.headSubject, err)
	}
	s.subscriptions = append(s.subscriptions, headSub)

	watchSub, err := s.bus.Subscribe(s.watchSubject, s.handleWatch)
	if err != nil {
		_ = headSub.Close()
		return fmt.Errorf("subscribe %s: %w", s.watchSubject, err)
	}
	s.subscriptions = append(s.subscriptions, watchSub)

	return nil
}

func (s *Server) handleHead(ctx context.Context, event core.ReceivedEvent) error {
	var req HeadRequest
	if err := json.Unmarshal(event.Payload, &req); err != nil {
		log.Printf("git-watchd: failed to unmarshal head request: %v", err)
		return nil
	}
	if req.ReplyTo == "" || req.ID == "" {
		log.Printf("git-watchd: ignoring head request without ID or ReplyTo: %+v", req)
		return nil
	}

	go func() {
		resp := HeadResponse{ID: req.ID}
		rev, err := s.engine.ResolveHead(context.Background(), req.Repo, req.Ref)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Revision = rev
		}

		payload, err := json.Marshal(resp)
		if err != nil {
			log.Printf("git-watchd: failed to marshal head response: %v", err)
			return
		}

		if _, err := s.bus.Publish(context.Background(), req.ReplyTo, payload, core.PublishOptions{
			TTL:         time.Minute,
			ContentType: "application/json",
		}); err != nil {
			log.Printf("git-watchd: failed to send head response to %s: %v", req.ReplyTo, err)
		}
	}()

	return nil
}

func (s *Server) handleWatch(ctx context.Context, event core.ReceivedEvent) error {
	var req WatchRequest
	if err := json.Unmarshal(event.Payload, &req); err != nil {
		log.Printf("git-watchd: failed to unmarshal watch request: %v", err)
		return nil
	}
	if req.ReplyTo == "" || req.ID == "" {
		log.Printf("git-watchd: ignoring watch request without ID or ReplyTo: %+v", req)
		return nil
	}

	go func() {
		// Use request's timeout plus buffer for watch execution context
		watchTimeout := DefaultTimeout
		if req.TimeoutMs > 0 {
			watchTimeout = time.Duration(req.TimeoutMs) * time.Millisecond
			if watchTimeout < MinTimeout {
				watchTimeout = MinTimeout
			} else if watchTimeout > MaxTimeout {
				watchTimeout = MaxTimeout
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), watchTimeout+5*time.Second)
		defer cancel()

		resp, err := s.watcher.Watch(ctx, req)
		if err != nil {
			resp = &WatchResponse{
				ID:     req.ID,
				Status: StatusError,
				Error:  err.Error(),
			}
		}

		payload, err := json.Marshal(resp)
		if err != nil {
			log.Printf("git-watchd: failed to marshal watch response: %v", err)
			return
		}

		if _, err := s.bus.Publish(context.Background(), req.ReplyTo, payload, core.PublishOptions{
			TTL:         time.Minute,
			ContentType: "application/json",
		}); err != nil {
			log.Printf("git-watchd: failed to send watch response to %s: %v", req.ReplyTo, err)
		}
	}()

	return nil
}

// Close closes all subscriptions.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	var errs []error
	for _, sub := range s.subscriptions {
		if err := sub.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing subscriptions: %v", errs)
	}
	return nil
}
