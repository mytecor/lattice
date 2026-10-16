package rns

import (
	"time"

	"github.com/Quad4-Software/Reticulum-Go/pkg/common"
	"github.com/Quad4-Software/Reticulum-Go/pkg/interfaces"
	"github.com/mytecor/meshbus/core"
)

const (
	defaultRealmAppName = "meshbus"
	defaultRealmAspect  = "peer"
	defaultAnnounce     = 5 * time.Minute
	defaultNetworkWait  = 30 * time.Second
)

// StackMode selects how the adapter joins Reticulum.
type StackMode uint8

const (
	// StackSharedClient attaches to an already-running shared instance and
	// never falls back to owning its listener.
	StackSharedClient StackMode = iota
	// StackStandalone starts the explicitly configured Reticulum interfaces.
	StackStandalone
)

// Config defines one generic Reticulum adapter endpoint. Shared-client mode
// uses the platform-default shared instance; standalone mode requires an
// explicit Reticulum configuration.
type Config struct {
	StackMode StackMode
	Reticulum *common.ReticulumConfig
	// connectShared is a test-only override for attaching shared-client mode
	// endpoints to an isolated shared-instance listener.
	connectShared sharedConnector
	// IdentitySource is an existing or new identity file path, or a private
	// RNS identity encoded in hex, Base32, or URL-safe Base64.
	IdentitySource string
	// EphemeralIdentity creates a fresh identity in memory and is mutually
	// exclusive with IdentitySource. The private identity is never persisted.
	EphemeralIdentity bool
	// RealmKey is the shared 256-bit membership secret. It is used only for
	// realm ID derivation and link challenge-response, and is never announced.
	RealmKey         []byte
	AppName          string
	Aspect           string
	AnnounceInterval time.Duration
	NetworkWait      time.Duration
	// Interfaces, when non-empty, replaces standalone config-driven interface
	// construction. It also supports wrapped interfaces for deterministic tests.
	Interfaces []interfaces.Interface
	// PresenceMetadata is bounded advisory data carried by meshbus.v1 presence.
	PresenceMetadata map[string]string
	// Passive suppresses local announces while retaining peer discovery.
	Passive bool
	// DirectoryConfig bounds the peer directory. Zero uses the package default.
	DirectoryConfig DirectoryConfig
	// Handler receives authenticated direct messages as meshbus primitives.
	Handler     core.Handler
	OnPeerError func(error)
}

// DirectoryConfig bounds the adapter's peer directory.
type DirectoryConfig struct {
	MaxPeers         int
	MaxMetadataBytes int
}
