package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
)

var (
	ErrInvalidPeerID  = errors.New("invalid peer identity")
	ErrInvalidMessage = errors.New("invalid direct message")
)

// MaxPeerIDBytes bounds identity material retained by the generic layer.
// Individual transports may impose a smaller bound.
const MaxPeerIDBytes = 256

// PeerID is an opaque identity established by the transport. Its binary form
// is kept immutable so application payloads cannot replace sender authority.
type PeerID struct {
	value string
}

// NewPeerID copies a non-empty transport-authenticated identity.
func NewPeerID(value []byte) (PeerID, error) {
	if len(value) == 0 || len(value) > MaxPeerIDBytes {
		return PeerID{}, ErrInvalidPeerID
	}
	return PeerID{value: string(bytes.Clone(value))}, nil
}

// Bytes returns a copy of the transport identity.
func (p PeerID) Bytes() []byte { return []byte(p.value) }

// String returns the identity in a stable, non-secret hexadecimal form.
func (p PeerID) String() string { return hex.EncodeToString([]byte(p.value)) }

// IsZero reports whether the identity was not constructed from authenticated
// transport bytes.
func (p PeerID) IsZero() bool { return p.value == "" }

// ReceivedMessage is an opaque payload paired with its transport-authenticated
// sender. The message does not interpret or trust identities inside Payload.
type ReceivedMessage struct {
	sender  PeerID
	payload []byte
}

// NewReceivedMessage constructs an isolated inbound message.
func NewReceivedMessage(sender []byte, payload []byte) (ReceivedMessage, error) {
	peer, err := NewPeerID(sender)
	if err != nil {
		return ReceivedMessage{}, err
	}
	return ReceivedMessage{sender: peer, payload: bytes.Clone(payload)}, nil
}

// Sender returns the authenticated transport peer.
func (m ReceivedMessage) Sender() PeerID { return m.sender }

// Payload returns a copy of the opaque application bytes.
func (m ReceivedMessage) Payload() []byte { return bytes.Clone(m.payload) }

// Handler receives an authenticated direct message.
type Handler func(context.Context, ReceivedMessage) error

// Sender sends opaque application bytes to an authenticated peer. Transport
// route resolution stays behind this boundary.
type Sender interface {
	SendMessage(context.Context, PeerID, []byte) error
}
