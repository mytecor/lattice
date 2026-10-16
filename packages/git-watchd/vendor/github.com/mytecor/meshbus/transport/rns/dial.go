package rns

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Quad4-Software/Reticulum-Go/pkg/destination"
	"github.com/Quad4-Software/Reticulum-Go/pkg/identity"
	"github.com/Quad4-Software/Reticulum-Go/pkg/link"
)

func (e *Endpoint) connect(ctx context.Context, destinationHash []byte, key string) (*session, error) {
	active, attempt, owner := e.connections.beginDial(key)
	if active != nil {
		return active, nil
	}
	if !owner {
		select {
		case <-attempt.done:
			return attempt.session, attempt.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	active, err := e.dial(ctx, destinationHash)
	e.connections.finishDial(key, attempt, active, err)
	return active, err
}

func (e *Endpoint) dial(ctx context.Context, destinationHash []byte) (*session, error) {
	if err := e.awaitPath(ctx, destinationHash); err != nil {
		return nil, fmt.Errorf("discover RNS path: %w", err)
	}
	remoteIdentity, err := identity.Recall(destinationHash)
	if err != nil {
		return nil, fmt.Errorf("recall announced identity: %w", err)
	}
	outbound, err := destination.FromHash(destinationHash, remoteIdentity, destination.Single, e.stack.transport)
	if err != nil {
		return nil, fmt.Errorf("construct outbound destination: %w", err)
	}
	established := make(chan *session, 1)
	failed := make(chan struct{}, 1)
	var outboundLink *link.Link
	outboundLink = link.NewLink(outbound, e.stack.transport, nil, func(value *link.Link) {
		active, setupErr := e.newSession(value, remoteIdentity.Hash(), destinationHash)
		if setupErr == nil {
			setupErr = value.Identify(e.identity)
		}
		if setupErr == nil {
			e.beginAuthentication(active)
		}
		if setupErr != nil {
			select {
			case failed <- struct{}{}:
			default:
			}
			return
		}
		select {
		case established <- active:
		default:
		}
	}, func(*link.Link) {
		select {
		case failed <- struct{}{}:
		default:
		}
	})
	if err := outboundLink.Establish(); err != nil {
		return nil, fmt.Errorf("establish RNS link: %w", err)
	}
	wait, cancel := boundedContext(ctx, e.networkWait)
	defer cancel()
	select {
	case active := <-established:
		return active, nil
	case <-failed:
		return nil, errors.New("RNS link closed before establishment")
	case <-wait.Done():
		outboundLink.Teardown()
		return nil, wait.Err()
	}
}

func (e *Endpoint) awaitPath(ctx context.Context, destinationHash []byte) error {
	if e.stack.transport.HasPath(destinationHash) {
		return nil
	}
	key := hex.EncodeToString(destinationHash)
	waiter := e.connections.addWaiter(key)
	defer e.connections.removeWaiter(key, waiter)
	if e.stack.transport.HasPath(destinationHash) {
		return nil
	}
	if err := e.stack.transport.RequestPath(destinationHash, "", nil, false); err != nil {
		return err
	}
	wait, cancel := boundedContext(ctx, e.networkWait)
	defer cancel()
	select {
	case <-waiter:
		if e.stack.transport.HasPath(destinationHash) {
			return nil
		}
		return errors.New("RNS announce did not install a path")
	case <-wait.Done():
		return wait.Err()
	}
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}
