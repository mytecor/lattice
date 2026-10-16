package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultPeerTTL       = 15 * time.Minute
	defaultSweepInterval = time.Minute
)

var (
	ErrInvalidNode        = errors.New("invalid meshbus node")
	ErrUnknownPeer        = errors.New("unknown meshbus peer")
	ErrNodeNotStarted     = errors.New("meshbus node is not started")
	ErrNodeAlreadyStarted = errors.New("meshbus node is already started")
	ErrNodeClosed         = errors.New("meshbus node is closed")
)

// NodeTransport supplies authenticated direct delivery and reports discovery
// separately from successful realm authentication.
type NodeTransport interface {
	Sender
	Identity() PeerID
	SetPeerObserver(PeerObserver) error
	Start(context.Context) error
	Close() error
}

type TransportFactory func(Handler) (NodeTransport, error)

type NodeConfig struct {
	Transport             TransportFactory
	DirectHandler         Handler
	Bus                   BusConfig
	Directory             DirectoryConfig
	PeerTTL               time.Duration
	SweepInterval         time.Duration
	InterestLeaseTTL      time.Duration
	InterestRenewInterval time.Duration
	OnPeerError           func(error)
}

type nodeState uint8

const (
	nodeCreated nodeState = iota
	nodeRunning
	nodeClosed
)

// Node owns authenticated peer state and composes it with direct messaging
// and bounded pub/sub. Transport discovery remains advisory until a realm
// proof promotes the candidate into Peers.
type Node struct {
	transport   NodeTransport
	bus         *Bus
	candidates  *PeerDirectory
	peers       *PeerDirectory
	identity    PeerID
	peerTTL     time.Duration
	sweep       time.Duration
	interests   *interestTable
	leaseTTL    time.Duration
	renewEvery  time.Duration
	onPeerError func(error)

	mu        sync.RWMutex
	state     nodeState
	runCtx    context.Context
	cancel    context.CancelFunc
	closeErr  error
	closeOnce sync.Once
}

func NewNode(config NodeConfig) (*Node, error) {
	if config.Transport == nil {
		return nil, fmt.Errorf("%w: transport factory is required", ErrInvalidNode)
	}
	if config.Bus.Sender != nil || config.Bus.Interests != nil {
		return nil, fmt.Errorf("%w: bus sender and interest source are managed by Node", ErrInvalidNode)
	}
	if config.PeerTTL == 0 {
		config.PeerTTL = defaultPeerTTL
	}
	if config.SweepInterval == 0 {
		config.SweepInterval = defaultSweepInterval
	}
	if config.InterestLeaseTTL == 0 {
		config.InterestLeaseTTL = defaultInterestLeaseTTL
	}
	if config.InterestRenewInterval == 0 {
		config.InterestRenewInterval = defaultInterestRenewInterval
	}
	if config.PeerTTL < time.Millisecond || config.SweepInterval < time.Millisecond {
		return nil, fmt.Errorf("%w: peer TTL and sweep interval must be positive", ErrInvalidNode)
	}
	if config.InterestLeaseTTL < time.Millisecond || config.InterestRenewInterval < time.Millisecond ||
		config.InterestRenewInterval >= config.InterestLeaseTTL ||
		config.InterestLeaseTTL > time.Duration(^uint32(0))*time.Millisecond {
		return nil, fmt.Errorf("%w: interest renew interval must be positive and shorter than the lease TTL", ErrInvalidNode)
	}

	candidates, err := NewPeerDirectory(config.Directory)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidNode, err)
	}
	authenticatedConfig := config.Directory
	authenticatedConfig.unbounded = true
	peers, err := NewPeerDirectory(authenticatedConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidNode, err)
	}
	interestClock := config.Bus.clock
	if interestClock == nil {
		interestClock = time.Now
	}
	node := &Node{
		candidates: candidates,
		peers:      peers,
		interests:  newInterestTable(interestClock, config.InterestLeaseTTL),
		peerTTL:    config.PeerTTL, sweep: config.SweepInterval, onPeerError: config.OnPeerError,
		leaseTTL: config.InterestLeaseTTL, renewEvery: config.InterestRenewInterval,
	}
	busConfig := config.Bus
	busConfig.Sender = node
	busConfig.Interests = node.interests
	bus, err := NewBus(busConfig)
	if err != nil {
		return nil, err
	}
	node.bus = bus
	transport, err := config.Transport(node.handler(config.DirectHandler))
	if err != nil {
		_ = bus.Close()
		return nil, fmt.Errorf("%w: construct transport: %w", ErrInvalidNode, err)
	}
	if transport == nil || transport.Identity().IsZero() {
		if transport != nil {
			_ = transport.Close()
		}
		_ = bus.Close()
		return nil, fmt.Errorf("%w: transport identity is required", ErrInvalidNode)
	}
	node.transport = transport
	node.identity = transport.Identity()
	if err := transport.SetPeerObserver(node); err != nil {
		_ = transport.Close()
		_ = bus.Close()
		return nil, fmt.Errorf("%w: bind peer observer: %w", ErrInvalidNode, err)
	}
	return node, nil
}

var (
	_ Sender       = (*Node)(nil)
	_ PeerObserver = (*Node)(nil)
)
