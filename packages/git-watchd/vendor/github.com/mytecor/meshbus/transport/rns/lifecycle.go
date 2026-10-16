package rns

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfig is returned for contradictory or invalid adapter config.
	ErrInvalidConfig = errors.New("invalid RNS adapter configuration")
	// ErrNotStarted is returned when a message is sent before Start.
	ErrNotStarted = errors.New("RNS adapter is not started")
	// ErrInvalidDestination is returned for a malformed destination hash.
	ErrInvalidDestination = errors.New("invalid RNS destination")
	// ErrClosed is returned when a message is sent on a closed adapter.
	ErrClosed = errors.New("RNS adapter is closed")
)

// Start starts Reticulum interfaces, publishes the presence descriptor, and
// refreshes it periodically.
func (e *Endpoint) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	if e.started {
		e.mu.Unlock()
		return nil
	}
	e.started = true
	e.mu.Unlock()
	if err := e.stack.Start(); err != nil {
		e.mu.Lock()
		e.started = false
		e.mu.Unlock()
		return fmt.Errorf("start Reticulum stack: %w", err)
	}
	if e.advertises {
		if err := e.destination.Announce(false, nil, nil); err != nil {
			_ = e.stack.Close()
			e.mu.Lock()
			e.started = false
			e.mu.Unlock()
			return fmt.Errorf("announce meshbus presence: %w", err)
		}
	}
	announceContext, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.stopAnnounce = cancel
	e.runContext = announceContext
	e.mu.Unlock()
	if e.advertises {
		go e.announceLoop(announceContext)
	}
	return nil
}

// Close shuts down links and the Reticulum stack. It is idempotent.
func (e *Endpoint) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	if e.stopAnnounce != nil {
		e.stopAnnounce()
	}
	e.mu.Unlock()
	for _, active := range e.connections.allSessions() {
		active.link.Teardown()
	}
	return e.stack.Close()
}
