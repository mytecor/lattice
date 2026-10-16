// Generic, transport-independent realm peer discovery.
//
// The entity here is a PeerDirectory, not a Cluster: the realm is a security
// boundary, whereas the directory is only the observable state of the network.
// A discovered peer carries a transport-authenticated public identity and
// bounded advisory metadata. Routes remain private to the transport adapter.
// Discovery is advisory only: an announce does not prove realm-key possession.
// Node promotes a candidate only after the transport reports successful realm
// authentication.
package core

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	// defaultDirectoryMaxPeers bounds how many peers one directory remembers.
	defaultDirectoryMaxPeers = 1024
	// defaultMetadataBytes bounds total application metadata carried on peer
	// records, so a peer cannot grow memory without limit.
	defaultMetadataBytes = 4096
)

var (
	// ErrInvalidPeer is returned when a peer record fails validation.
	ErrInvalidPeer = errors.New("invalid discovered peer")
	// ErrMetadataLimit is returned when application metadata exceeds bounds.
	ErrMetadataLimit = errors.New("peer metadata exceeds bound")
	// ErrInvalidDirectoryConfig is returned for negative resource bounds.
	ErrInvalidDirectoryConfig = errors.New("invalid peer directory configuration")
)

// Peer describes one authenticated realm peer learned through discovery.
type Peer struct {
	// ID is the authenticated public identity established by the transport.
	// It never comes from application metadata on the wire.
	ID PeerID
	// Metadata holds optional bounded application metadata (for example
	// advisory os/arch hints). Presence and metadata are advisory only.
	Metadata map[string]string
	// Hops is an optional advisory path metric such as hop count.
	Hops uint8
	// LastSeen is the local time the peer was last (re)discovered.
	LastSeen time.Time
}

// PeerObserver receives transport discovery and successful realm
// authentication separately. Discovery is advisory; only Authenticated proves
// that a transport identity holds the realm key.
type PeerObserver interface {
	Discovered(Peer) error
	Authenticated(PeerID) error
}

// isValid reports whether the peer carries a valid authenticated identity.
func (p Peer) isValid() bool { return !p.ID.IsZero() }

// metadataBytes returns the total UTF-8 byte size of the metadata map.
func metadataBytes(metadata map[string]string) int {
	total := 0
	for key, value := range metadata {
		total += len(key) + len(value)
	}
	return total
}

// DirectoryConfig sets the finite resource bounds for one peer directory.
type DirectoryConfig struct {
	// MaxPeers bounds the number of remembered peers.
	MaxPeers int
	// MaxMetadataBytes bounds the total application metadata carried on each
	// peer record. Zero uses the default bound.
	MaxMetadataBytes int

	now func() time.Time
	// unbounded is used for realm-authenticated peers owned by Node. Discovery
	// directories remain bounded by MaxPeers.
	unbounded bool
}

func (c DirectoryConfig) apply() (DirectoryConfig, error) {
	if c.MaxPeers < 0 || c.MaxMetadataBytes < 0 {
		return DirectoryConfig{}, ErrInvalidDirectoryConfig
	}
	if c.MaxPeers == 0 && !c.unbounded {
		c.MaxPeers = defaultDirectoryMaxPeers
	}
	if c.MaxMetadataBytes == 0 {
		c.MaxMetadataBytes = defaultMetadataBytes
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// PeerDirectory is an observable record of realm peers learned through
// discovery or authentication. Discovery directories are bounded with
// oldest-first eviction; Node's authenticated directory is unbounded. It is
// safe for concurrent use.
type PeerDirectory struct {
	config DirectoryConfig

	mu    sync.RWMutex
	peers map[PeerID]Peer
}

// NewPeerDirectory creates an empty directory with the given resource bounds.
func NewPeerDirectory(config DirectoryConfig) (*PeerDirectory, error) {
	config, err := config.apply()
	if err != nil {
		return nil, err
	}
	return &PeerDirectory{
		config: config,
		peers:  make(map[PeerID]Peer),
	}, nil
}

// Remember adds or updates one discovered peer. An existing peer with the same
// authenticated PeerID is updated in place instead of creating a duplicate.
// The route may change while the identity stays stable. A bounded directory
// evicts its oldest record when a new identity arrives at capacity.
func (d *PeerDirectory) Remember(peer Peer) error {
	if !peer.isValid() {
		return fmt.Errorf("%w: identity is required and must be authenticated", ErrInvalidPeer)
	}
	if peer.Metadata == nil {
		peer.Metadata = map[string]string{}
	}
	if n := metadataBytes(peer.Metadata); n > d.config.MaxMetadataBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrMetadataLimit, n, d.config.MaxMetadataBytes)
	}
	// Snapshot the metadata so the caller cannot mutate the stored record.
	peer.Metadata = cloneMetadata(peer.Metadata)
	peer.LastSeen = d.config.now()

	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.peers[peer.ID]; !exists && !d.config.unbounded && len(d.peers) >= d.config.MaxPeers {
		var oldestID PeerID
		var oldest Peer
		for id, candidate := range d.peers {
			if oldestID.IsZero() || candidate.LastSeen.Before(oldest.LastSeen) ||
				(candidate.LastSeen.Equal(oldest.LastSeen) && bytes.Compare(id.Bytes(), oldestID.Bytes()) < 0) {
				oldestID, oldest = id, candidate
			}
		}
		delete(d.peers, oldestID)
	}
	d.peers[peer.ID] = peer
	return nil
}

// Get returns an immutable copy of one discovered peer.
func (d *PeerDirectory) Get(id PeerID) (Peer, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	peer, exists := d.peers[id]
	if !exists {
		return Peer{}, false
	}
	return clonePeer(peer), true
}

// Peers returns an immutable, copy-safe snapshot ordered by identity for
// deterministic fan-out. Discovery is advisory only.
func (d *PeerDirectory) Peers() []Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]Peer, 0, len(d.peers))
	for _, peer := range d.peers {
		result = append(result, clonePeer(peer))
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].ID.Bytes(), result[j].ID.Bytes()) < 0
	})
	return result
}

// IDs returns the peer identity snapshot used for pub/sub fan-out, ordered by
// identity.
func (d *PeerDirectory) IDs() []PeerID {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]PeerID, 0, len(d.peers))
	for _, peer := range d.peers {
		result = append(result, peer.ID)
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].Bytes(), result[j].Bytes()) < 0
	})
	return result
}

// Remove forgets one peer. It is a no-op when the peer is unknown.
func (d *PeerDirectory) Remove(id PeerID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.peers, id)
}

// ExpireStale removes peers whose LastSeen is older than the given age. With a
// zero age only zero-valued LastSeen records age out. It updates LastSeen on
// every Remember, so idle peers are what age out.
func (d *PeerDirectory) ExpireStale(olderThan time.Duration) int {
	cutoff := d.config.now().Add(-olderThan)
	d.mu.Lock()
	defer d.mu.Unlock()
	removed := 0
	for id, peer := range d.peers {
		if peer.LastSeen.Before(cutoff) {
			delete(d.peers, id)
			removed++
		}
	}
	return removed
}

// Len returns the number of remembered peers.
func (d *PeerDirectory) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.peers)
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	clone := make(map[string]string, len(metadata))
	for key, value := range metadata {
		clone[key] = value
	}
	return clone
}

func clonePeer(peer Peer) Peer {
	peer.Metadata = cloneMetadata(peer.Metadata)
	return peer
}
