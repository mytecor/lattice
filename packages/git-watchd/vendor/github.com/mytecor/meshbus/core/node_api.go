package core

import (
	"context"
	"fmt"
)

func (n *Node) Send(ctx context.Context, peer PeerID, payload []byte) error {
	if peer.IsZero() {
		return ErrInvalidPeerID
	}
	if err := n.requireRunning(); err != nil {
		return err
	}
	if _, ok := n.peers.Get(peer); !ok {
		if _, candidate := n.candidates.Get(peer); !candidate {
			return fmt.Errorf("%w: %s", ErrUnknownPeer, peer.String())
		}
	}
	return n.transport.SendMessage(ctx, peer, payload)
}

// SendMessage implements Sender for the composed Bus.
func (n *Node) SendMessage(ctx context.Context, peer PeerID, payload []byte) error {
	return n.Send(ctx, peer, payload)
}

func (n *Node) Identity() PeerID { return n.identity }

func (n *Node) Subscribe(pattern string, handler EventHandler) (*Subscription, error) {
	subscription, err := n.bus.Subscribe(pattern, handler)
	if err != nil {
		return nil, err
	}
	if n.isRunning() {
		n.broadcastInterests([]string{pattern})
	}
	return subscription, nil
}

func (n *Node) Publish(ctx context.Context, topic string, payload []byte, options PublishOptions) (PublishResult, error) {
	if err := n.requireRunning(); err != nil {
		return PublishResult{}, err
	}
	if !options.RemoteOnly {
		options.localSender = n.identity
	}
	return n.bus.Publish(ctx, topic, payload, options)
}

// DiscoveredPeers returns advisory presence candidates.
func (n *Node) DiscoveredPeers() []Peer { return n.candidates.Peers() }

// Peers returns only identities that completed realm authentication.
func (n *Node) Peers() []Peer { return n.peers.Peers() }
