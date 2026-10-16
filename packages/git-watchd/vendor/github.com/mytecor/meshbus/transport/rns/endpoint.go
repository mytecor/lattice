package rns

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Quad4-Software/Reticulum-Go/pkg/destination"
	"github.com/Quad4-Software/Reticulum-Go/pkg/identity"
	"github.com/mytecor/meshbus/core"
	"github.com/mytecor/meshbus/security/realm"
)

// Endpoint is the reusable meshbus transport facade over authenticated RNS
// direct messages. Link establishment, realm authentication, Channel delivery,
// discovery and session reuse remain behind it.
type Endpoint struct {
	mu          sync.Mutex
	stack       *stack
	identity    *identity.Identity
	destination *destination.Destination
	handler     core.Handler
	interval    time.Duration
	networkWait time.Duration
	name        string
	advertises  bool
	realm       *realm.Realm
	realmID     []byte
	codec       genericPresenceCodec
	directory   *core.PeerDirectory
	observer    core.PeerObserver
	onPeerError func(error)
	runContext  context.Context

	started      bool
	closed       bool
	connections  *connectionRegistry
	stopAnnounce context.CancelFunc
}

// New constructs an adapter endpoint without starting network interfaces. It
// validates realm membership and bounded presence metadata before returning.
func New(config Config) (*Endpoint, error) {
	if config.Handler == nil {
		return nil, fmt.Errorf("%w: handler is required", ErrInvalidConfig)
	}
	if config.EphemeralIdentity == (strings.TrimSpace(config.IdentitySource) != "") {
		return nil, fmt.Errorf("%w: exactly one of identity source or ephemeral identity is required", ErrInvalidConfig)
	}
	openedRealm, err := realm.Open(realm.Config{Key: config.RealmKey})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	realmID := openedRealm.ID()

	codec := genericPresenceCodec{metadata: config.PresenceMetadata, passive: config.Passive}
	if config.AppName == "" {
		config.AppName = defaultRealmAppName
	}
	if config.Aspect == "" {
		config.Aspect = defaultRealmAspect
	}
	if config.AnnounceInterval == 0 {
		config.AnnounceInterval = defaultAnnounce
	}
	if config.AnnounceInterval < 0 {
		return nil, fmt.Errorf("%w: announce interval must be positive", ErrInvalidConfig)
	}
	if config.NetworkWait == 0 {
		config.NetworkWait = defaultNetworkWait
	}
	if config.NetworkWait < 0 {
		return nil, fmt.Errorf("%w: network wait must be positive", ErrInvalidConfig)
	}

	var localIdentity *identity.Identity
	var loadErr error
	if config.EphemeralIdentity {
		localIdentity, loadErr = identity.New()
		if loadErr != nil {
			return nil, fmt.Errorf("generate ephemeral identity: %w", loadErr)
		}
	} else {
		localIdentity, loadErr = loadOrCreateIdentity(config.IdentitySource)
		if loadErr != nil {
			return nil, fmt.Errorf("load identity: %w", loadErr)
		}
	}
	rnsStack, err := newStack(config.StackMode, config.Reticulum, config.Interfaces...)
	if err != nil {
		return nil, fmt.Errorf("construct Reticulum stack: %w", err)
	}
	if config.connectShared != nil {
		if !rnsStack.required {
			return nil, fmt.Errorf("%w: shared-instance connector cannot be combined with a standalone Reticulum config", ErrInvalidConfig)
		}
		rnsStack.connect = config.connectShared
	}
	localDestination, err := destination.New(localIdentity, destination.In, destination.Single, config.AppName, rnsStack.transport, config.Aspect)
	if err != nil {
		return nil, fmt.Errorf("construct RNS destination: %w", err)
	}
	localDestination.AcceptsLinks(true)

	// Build announcement presence once. Passive endpoints produce no app_data
	// and do not advertise.
	advertiseData, err := codec.Build(realmID, localIdentity.Hash())
	if err != nil {
		return nil, fmt.Errorf("%w: build presence: %v", ErrInvalidConfig, err)
	}
	localDestination.SetDefaultAppData(advertiseData)

	directory, err := core.NewPeerDirectory(core.DirectoryConfig{
		MaxPeers:         config.DirectoryConfig.MaxPeers,
		MaxMetadataBytes: config.DirectoryConfig.MaxMetadataBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: peer directory: %v", ErrInvalidConfig, err)
	}
	endpoint := &Endpoint{
		stack: rnsStack, identity: localIdentity, destination: localDestination,
		handler: config.Handler, interval: config.AnnounceInterval, networkWait: config.NetworkWait,
		onPeerError: config.OnPeerError,
		name:        hex.EncodeToString(localIdentity.Hash()), realm: openedRealm, realmID: realmID,
		codec: codec, advertises: len(advertiseData) > 0,
		connections: newConnectionRegistry(),
		directory:   directory,
	}
	localDestination.SetLinkEstablishedCallback(endpoint.acceptLink)
	aspect := config.AppName + "." + config.Aspect
	rnsStack.transport.RegisterAnnounceHandler(&announceHandler{endpoint: endpoint, aspect: aspect})
	return endpoint, nil
}

func (e *Endpoint) reportPeerError(err error) {
	if err != nil && e.onPeerError != nil {
		e.onPeerError(err)
	}
}

// Name is the hex-encoded hash of the RNS identity.
func (e *Endpoint) Name() string { return e.name }

// Destination is the hex-encoded destination hash clients use for their first connection.
func (e *Endpoint) Destination() string { return hex.EncodeToString(e.destination.GetHash()) }

// Identity returns the local transport-authenticated peer identity.
func (e *Endpoint) Identity() core.PeerID {
	peer, _ := core.NewPeerID(e.identity.Hash())
	return peer
}

// RealmID returns a copy of the adapter's public realm identifier.
func (e *Endpoint) RealmID() []byte { return bytes.Clone(e.realmID) }

// DiscoveredPeers returns advisory announce candidates. Realm authentication
// has not necessarily completed; Node promotes candidates after proof.
func (e *Endpoint) DiscoveredPeers() []core.Peer { return e.directory.Peers() }

// SetPeerObserver binds discovery and realm-authentication events before Start.
func (e *Endpoint) SetPeerObserver(observer core.PeerObserver) error {
	if observer == nil {
		return fmt.Errorf("%w: peer observer is required", ErrInvalidConfig)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if e.started {
		return fmt.Errorf("%w: peer observer must be set before Start", ErrInvalidConfig)
	}
	e.observer = observer
	return nil
}

func (e *Endpoint) handlerContext() context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runContext != nil {
		return e.runContext
	}
	return context.Background()
}

var _ core.Sender = (*Endpoint)(nil)
var _ core.NodeTransport = (*Endpoint)(nil)
