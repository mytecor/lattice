package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/mytecor/meshbus/internal/protocol/subject"
	"github.com/mytecor/meshbus/internal/protocol/wire"
)

const MaxTopicBytes = subject.MaxBytes

var (
	ErrInvalidEvent      = wire.ErrInvalidFrame
	ErrInvalidTopic      = subject.ErrInvalidTopic
	ErrInvalidPattern    = subject.ErrInvalidPattern
	ErrEventBackpressure = errors.New("event subscription queue is full")
	ErrBusClosed         = errors.New("event bus is closed")
)

// EventID is a publisher-generated 128-bit identifier used for bounded
// duplicate suppression. It is not an authority token.
type EventID [16]byte

func (id EventID) String() string { return hex.EncodeToString(id[:]) }

// Event is the transport-neutral event data. PublishedAt and other serialized
// fields are metadata; ReceivedEvent.Sender is the authoritative peer.
type Event struct {
	ID          EventID
	Topic       string
	PublishedAt time.Time
	TTL         time.Duration
	ContentType string
	Payload     []byte
}

// ReceivedEvent pairs an event with transport-authenticated delivery metadata.
type ReceivedEvent struct {
	Event
	Sender     PeerID
	ReceivedAt time.Time
}

// EventHandler handles one event. Duplicate delivery remains possible across
// process restarts and must be safe at the application boundary.
type EventHandler func(context.Context, ReceivedEvent) error

// PublishOptions controls bounded event lifetime and payload interpretation.
type PublishOptions struct {
	TTL         time.Duration
	ContentType string
	// RemoteOnly suppresses delivery to local subscriptions when publishing
	// through Node. Direct Bus users have no local identity and remain remote-only.
	RemoteOnly bool

	localSender PeerID
}

// PublishResult reports best-effort delivery without claiming remote
// acknowledgement or exactly-once semantics.
type PublishResult struct {
	ID             EventID
	Attempted      int
	Delivered      int
	Failed         map[PeerID]error
	LocalDelivered bool
	LocalError     error
}

func validateTopic(topic string) error {
	return subject.ValidateTopic(topic)
}

func validatePattern(pattern string) error {
	return subject.ValidatePattern(pattern)
}

func matchPattern(pattern, topic string) bool {
	return subject.Match(pattern, topic)
}

func cloneEvent(event Event) Event {
	event.Payload = bytes.Clone(event.Payload)
	return event
}

func cloneReceivedEvent(event ReceivedEvent) ReceivedEvent {
	event.Event = cloneEvent(event.Event)
	return event
}
