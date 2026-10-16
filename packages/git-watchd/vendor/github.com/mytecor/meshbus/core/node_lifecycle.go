package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (n *Node) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidNode)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	switch n.state {
	case nodeRunning:
		return ErrNodeAlreadyStarted
	case nodeClosed:
		return ErrNodeClosed
	}
	runContext, cancel := context.WithCancel(ctx)
	if err := n.transport.Start(runContext); err != nil {
		cancel()
		return err
	}
	n.cancel = cancel
	n.runCtx = runContext
	n.state = nodeRunning
	go n.sweepLoop(runContext)
	go n.interestLoop(runContext)
	go n.reconcileInterests()
	return nil
}

func (n *Node) Close() error {
	n.closeOnce.Do(func() {
		n.mu.Lock()
		n.state = nodeClosed
		if n.cancel != nil {
			n.cancel()
		}
		n.mu.Unlock()
		n.closeErr = errors.Join(n.bus.Close(), n.transport.Close())
	})
	return n.closeErr
}

func (n *Node) requireRunning() error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	switch n.state {
	case nodeRunning:
		return nil
	case nodeClosed:
		return ErrNodeClosed
	default:
		return ErrNodeNotStarted
	}
}

func (n *Node) isRunning() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state == nodeRunning
}

func (n *Node) runContext() context.Context {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.state != nodeRunning || n.runCtx == nil {
		return nil
	}
	return n.runCtx
}

func (n *Node) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(n.sweep)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.candidates.ExpireStale(n.peerTTL)
		}
	}
}
