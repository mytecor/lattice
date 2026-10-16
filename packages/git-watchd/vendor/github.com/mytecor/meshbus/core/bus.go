package core

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	defaultEventTTL          = time.Minute
	defaultMaxEventTTL       = time.Hour
	defaultMaxEventPayload   = 64 * 1024 * 1024
	defaultDedupCapacity     = 4096
	defaultQueueCapacity     = 32
	defaultFanoutConcurrency = 8
)

// InterestSource returns authenticated peers with a current leased interest
// matching one concrete topic.
type InterestSource interface {
	InterestedPeers(string) []PeerID
}

// InterestSourceFunc adapts a function to InterestSource.
type InterestSourceFunc func(string) []PeerID

func (f InterestSourceFunc) InterestedPeers(topic string) []PeerID { return f(topic) }

// BusConfig sets finite resource bounds for one in-memory event bus.
type BusConfig struct {
	Sender            Sender
	Interests         InterestSource
	DefaultTTL        time.Duration
	MaxTTL            time.Duration
	MaxPayloadBytes   int
	DedupCapacity     int
	QueueCapacity     int
	FanoutConcurrency int
	OnHandlerError    func(error)

	clock    func() time.Time
	idSource io.Reader
}

// Bus distributes best-effort events over authenticated direct messages.
// It owns no durable log, replay cursor, or consumer group state.
type Bus struct {
	sender    Sender
	interests InterestSource
	config    BusConfig

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.RWMutex
	closed bool
	nextID uint64
	subs   map[uint64]*Subscription

	dedupMu sync.Mutex
	seen    map[EventID]time.Time
}

// Subscription is one subject-pattern handler with a bounded private queue.
type Subscription struct {
	bus     *Bus
	id      uint64
	pattern string
	handler EventHandler
	queue   chan ReceivedEvent
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

// NewBus creates a stopped-empty but immediately usable event bus.
func NewBus(config BusConfig) (*Bus, error) {
	if config.Sender == nil || config.Interests == nil {
		return nil, fmt.Errorf("%w: sender and interest source are required", ErrInvalidEvent)
	}
	if config.DefaultTTL == 0 {
		config.DefaultTTL = defaultEventTTL
	}
	if config.MaxTTL == 0 {
		config.MaxTTL = defaultMaxEventTTL
	}
	if config.MaxPayloadBytes == 0 {
		config.MaxPayloadBytes = defaultMaxEventPayload
	}
	if config.DedupCapacity == 0 {
		config.DedupCapacity = defaultDedupCapacity
	}
	if config.QueueCapacity == 0 {
		config.QueueCapacity = defaultQueueCapacity
	}
	if config.FanoutConcurrency == 0 {
		config.FanoutConcurrency = defaultFanoutConcurrency
	}
	if config.DefaultTTL < time.Millisecond || config.MaxTTL < config.DefaultTTL ||
		config.MaxTTL > time.Duration(^uint32(0))*time.Millisecond || config.MaxPayloadBytes < 1 ||
		uint64(config.MaxPayloadBytes) > uint64(^uint32(0)) ||
		config.DedupCapacity < 1 || config.QueueCapacity < 1 || config.FanoutConcurrency < 1 {
		return nil, fmt.Errorf("%w: invalid event bus bounds", ErrInvalidEvent)
	}
	if config.clock == nil {
		config.clock = time.Now
	}
	if config.idSource == nil {
		config.idSource = rand.Reader
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Bus{
		sender: config.Sender, interests: config.Interests, config: config,
		ctx: ctx, cancel: cancel, subs: make(map[uint64]*Subscription), seen: make(map[EventID]time.Time),
	}, nil
}

// Close stops subscriptions and rejects future publication or registration.
func (b *Bus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.cancel()
	subscriptions := make([]*Subscription, 0, len(b.subs))
	for _, subscription := range b.subs {
		subscriptions = append(subscriptions, subscription)
		subscription.cancel()
	}
	b.subs = make(map[uint64]*Subscription)
	b.mu.Unlock()
	for _, subscription := range subscriptions {
		subscription.wg.Wait()
	}
	return nil
}
