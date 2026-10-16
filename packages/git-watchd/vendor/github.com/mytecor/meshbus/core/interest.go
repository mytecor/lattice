package core

import (
	"sync"
	"time"
)

const (
	defaultInterestLeaseTTL      = 60 * time.Second
	defaultInterestRenewInterval = 20 * time.Second
)

// interestTable is soft routing state learned directly from authenticated
// peers. Entries disappear only through lease expiry, never because a send
// failed or transport presence changed.
type interestTable struct {
	mu       sync.Mutex
	byPeer   map[PeerID]map[string]time.Time
	clock    func() time.Time
	maxLease time.Duration
}

func newInterestTable(clock func() time.Time, maxLease time.Duration) *interestTable {
	if clock == nil {
		clock = time.Now
	}
	return &interestTable{byPeer: make(map[PeerID]map[string]time.Time), clock: clock, maxLease: maxLease}
}

func (t *interestTable) renew(peer PeerID, patterns []string, lease time.Duration) {
	if peer.IsZero() || lease <= 0 {
		return
	}
	if lease > t.maxLease {
		lease = t.maxLease
	}
	expiresAt := t.clock().Add(lease)
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := t.byPeer[peer]
	if entries == nil {
		entries = make(map[string]time.Time)
		t.byPeer[peer] = entries
	}
	for _, pattern := range patterns {
		if validatePattern(pattern) == nil {
			entries[pattern] = expiresAt
		}
	}
}

func (t *interestTable) InterestedPeers(topic string) []PeerID {
	now := t.clock()
	t.mu.Lock()
	defer t.mu.Unlock()
	peers := make([]PeerID, 0, len(t.byPeer))
	for peer, entries := range t.byPeer {
		matched := false
		for pattern, expiresAt := range entries {
			if !expiresAt.After(now) {
				delete(entries, pattern)
				continue
			}
			if matchPattern(pattern, topic) {
				matched = true
			}
		}
		if len(entries) == 0 {
			delete(t.byPeer, peer)
		}
		if matched {
			peers = append(peers, peer)
		}
	}
	return peers
}

var _ InterestSource = (*interestTable)(nil)
