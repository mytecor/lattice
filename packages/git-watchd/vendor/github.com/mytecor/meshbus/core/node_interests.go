package core

import (
	"context"
	"fmt"
	"time"
)

func (n *Node) handler(next Handler) Handler {
	fallback := n.bus.Handler(next)
	return func(ctx context.Context, message ReceivedMessage) error {
		frame := message.Payload()
		if isInterestMessage(frame) {
			control, err := decodeInterest(frame)
			if err != nil {
				return err
			}
			if _, authenticated := n.peers.Get(message.Sender()); !authenticated {
				return fmt.Errorf("%w: interest from unauthenticated peer %s", ErrUnknownPeer, message.Sender())
			}
			switch control.kind {
			case interestKindQuery:
				return n.sendInterests(ctx, message.Sender(), n.bus.patterns())
			case interestKindRenew:
				n.interests.renew(message.Sender(), control.patterns, control.lease)
				return nil
			}
		}
		return fallback(ctx, message)
	}
}

func (n *Node) interestLoop(ctx context.Context) {
	ticker := time.NewTicker(n.renewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.broadcastInterests(n.bus.patterns())
		}
	}
}

func (n *Node) reconcileInterests() {
	for _, peer := range n.peerIDs() {
		n.syncInterests(peer)
	}
}

func (n *Node) syncInterests(peer PeerID) {
	ctx := n.runContext()
	if ctx == nil {
		return
	}
	if err := n.transport.SendMessage(ctx, peer, encodeInterestQuery()); err != nil {
		n.reportPeerError(fmt.Errorf("query interests from %s: %w", peer, err))
	}
	if err := n.sendInterests(ctx, peer, n.bus.patterns()); err != nil {
		n.reportPeerError(fmt.Errorf("announce interests to %s: %w", peer, err))
	}
}

func (n *Node) broadcastInterests(patterns []string) {
	if len(patterns) == 0 {
		return
	}
	ctx := n.runContext()
	if ctx == nil {
		return
	}
	for _, peer := range n.peerIDs() {
		if err := n.sendInterests(ctx, peer, patterns); err != nil {
			n.reportPeerError(fmt.Errorf("renew interests to %s: %w", peer, err))
		}
	}
}

func (n *Node) sendInterests(ctx context.Context, peer PeerID, patterns []string) error {
	for start := 0; start < len(patterns); start += maxPatternsPerRenew {
		end := min(start+maxPatternsPerRenew, len(patterns))
		frame, err := encodeInterestRenew(patterns[start:end], n.leaseTTL)
		if err != nil {
			return err
		}
		if err := n.transport.SendMessage(ctx, peer, frame); err != nil {
			return err
		}
	}
	return nil
}
