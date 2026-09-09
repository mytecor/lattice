package main

import (
	"sync"
	"time"
)

// LeaseStore holds the current winner lease per logical model. A lease
// temporarily promotes the winning provider to the top of the ranking, is
// renewed by successful responses, and is released on configured hard failures
// or after a number of consecutive slow starts. Loser cancellations never touch
// lease state.
type LeaseStore struct {
	mu      sync.Mutex
	entries map[string]leaseEntry
	now     func() time.Time
}

type leaseEntry struct {
	provider   string
	expiresAt  time.Time
	slowStarts int
}

func newLeaseStore(now func() time.Time) *LeaseStore {
	if now == nil {
		now = time.Now
	}
	return &LeaseStore{entries: make(map[string]leaseEntry), now: now}
}

// Holder returns the currently leased provider for the logical model if the
// lease has not expired.
func (l *LeaseStore) Holder(logical string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[logical]
	if !ok {
		return "", false
	}
	if !l.now().Before(entry.expiresAt) {
		delete(l.entries, logical)
		return "", false
	}
	return entry.provider, true
}

// Exists reports whether the model currently holds a live lease.
func (l *LeaseStore) Exists(logical string) bool {
	_, ok := l.Holder(logical)
	return ok
}

// Renew sets or extends the lease for the winning provider, preserving the
// consecutive slow-start counter of the same holder.
func (l *LeaseStore) Renew(logical, provider string, duration time.Duration) {
	if provider == "" || duration <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := leaseEntry{provider: provider, expiresAt: l.now().Add(duration)}
	if existing, ok := l.entries[logical]; ok && existing.provider == provider {
		entry.slowStarts = existing.slowStarts
	}
	l.entries[logical] = entry
}

// ReleaseIfHolder drops the lease only when the failing provider is its holder.
// Cancelled losers never reach this method (the scheduler filters them).
func (l *LeaseStore) ReleaseIfHolder(logical, provider string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.entries[logical]; ok && entry.provider == provider {
		delete(l.entries, logical)
	}
}

// ObserveSlowStart advances the consecutive slow-start counter of the holder;
// releasing the lease once the holder reaches the threshold. A fast start by
// the holder resets the counter. Slow starts by non-holders are neutral.
func (l *LeaseStore) ObserveSlowStart(logical, provider string, threshold int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[logical]
	if !ok || entry.provider != provider {
		return
	}
	entry.slowStarts++
	if entry.slowStarts >= threshold {
		delete(l.entries, logical)
		return
	}
	l.entries[logical] = entry
}

// ResetSlowStarts clears the consecutive slow-start counter for the holder.
func (l *LeaseStore) ResetSlowStarts(logical, provider string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.entries[logical]; ok && entry.provider == provider {
		entry.slowStarts = 0
		l.entries[logical] = entry
	}
}

// HolderCount reports the number of live leases (for tests and metrics).
func (l *LeaseStore) HolderCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	count := 0
	for logical, entry := range l.entries {
		if now.Before(entry.expiresAt) {
			count++
		} else {
			delete(l.entries, logical)
		}
	}
	return count
}
