package rns

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Quad4-Software/Reticulum-Go/pkg/link"
	"github.com/Quad4-Software/Reticulum-Go/pkg/resource"
	"github.com/mytecor/meshbus/core"
)

// DestinationForIdentity returns the last authenticated destination learned
// through an announce or an outbound session.
func (e *Endpoint) DestinationForIdentity(identityHash string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(identityHash))
	destinationHash, ok := e.connections.destination(key)
	if !ok {
		return "", false
	}
	return hex.EncodeToString(destinationHash), true
}

// SendMessage resolves an authenticated peer to its current RNS destination.
func (e *Endpoint) SendMessage(ctx context.Context, peer core.PeerID, payload []byte) error {
	if peer.IsZero() {
		return core.ErrInvalidPeerID
	}
	target, ok := e.DestinationForIdentity(peer.String())
	if !ok {
		return fmt.Errorf("%w: %s", core.ErrUnknownPeer, peer)
	}
	return e.SendToDestination(ctx, target, payload)
}

// SendToDestination sends over a realm-authenticated RNS session.
func (e *Endpoint) SendToDestination(ctx context.Context, target string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	destinationHash, key, err := parseDestination(target)
	if err != nil {
		return err
	}
	if err := e.requireStarted(); err != nil {
		return err
	}
	destinationHash, key, active := e.connections.resolve(destinationHash, key)
	if active == nil || active.link.GetStatus() != link.StatusActive {
		active, err = e.connect(ctx, destinationHash, key)
		if err != nil {
			return err
		}
	}
	if err := e.waitAuthenticated(ctx, active); err != nil {
		return fmt.Errorf("authenticate RNS session: %w", err)
	}
	if len(payload) <= active.channel.MDU() {
		if err := e.sendChannel(ctx, active, &directMessage{data: bytes.Clone(payload)}); err != nil {
			return fmt.Errorf("send RNS Channel message: %w", err)
		}
		return nil
	}
	return e.sendResource(ctx, active, payload)
}

func (e *Endpoint) requireStarted() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if !e.started {
		return ErrNotStarted
	}
	return nil
}

func (e *Endpoint) sendResource(ctx context.Context, active *session, payload []byte) error {
	transfer, err := resource.New(bytes.Clone(payload), true)
	if err != nil {
		return fmt.Errorf("create RNS Resource: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { result <- active.link.SendResource(transfer) }()
	select {
	case err := <-result:
		if err != nil {
			return fmt.Errorf("send RNS Resource: %w", err)
		}
		return nil
	case <-ctx.Done():
		transfer.Cancel()
		return ctx.Err()
	}
}

func parseDestination(value string) ([]byte, string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 16 {
		return nil, "", fmt.Errorf("%w: expected a 32-character hex hash", ErrInvalidDestination)
	}
	return decoded, value, nil
}
