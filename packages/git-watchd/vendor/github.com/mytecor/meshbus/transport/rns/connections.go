package rns

import (
	"bytes"
	"encoding/hex"
	"sync"

	"github.com/Quad4-Software/Reticulum-Go/pkg/channel"
	"github.com/Quad4-Software/Reticulum-Go/pkg/link"
)

type session struct {
	mu            sync.RWMutex
	link          *link.Link
	channel       *channel.Channel
	sender        []byte
	pendingAuth   map[byte]authMessage
	challenge     []byte
	authenticated bool
	authErr       error
	authDone      chan struct{}
	authOnce      sync.Once
	peerReady     bool
	readyDone     chan struct{}
	readyOnce     sync.Once
}

type dialAttempt struct {
	done    chan struct{}
	session *session
	err     error
}

// connectionRegistry owns the concurrent routing and connection state. It is
// deliberately independent from Endpoint's start/close lifecycle lock.
type connectionRegistry struct {
	mu           sync.Mutex
	sessions     map[string]*session
	destinations map[string][]byte
	dials        map[string]*dialAttempt
	waiters      map[string][]chan struct{}
}

func newConnectionRegistry() *connectionRegistry {
	return &connectionRegistry{
		sessions:     make(map[string]*session),
		destinations: make(map[string][]byte),
		dials:        make(map[string]*dialAttempt),
		waiters:      make(map[string][]chan struct{}),
	}
}

func (r *connectionRegistry) destination(identity string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.destinations[identity]
	return bytes.Clone(value), len(value) == 16
}

func (r *connectionRegistry) resolve(destinationHash []byte, key string) ([]byte, string, *session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	active := r.sessions[key]
	if mapped := r.destinations[key]; len(mapped) == 16 {
		destinationHash = bytes.Clone(mapped)
		key = hex.EncodeToString(destinationHash)
		if active == nil {
			active = r.sessions[key]
		}
	}
	return destinationHash, key, active
}

func (r *connectionRegistry) beginDial(key string) (*session, *dialAttempt, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if active := r.sessions[key]; active != nil && active.link.GetStatus() == link.StatusActive {
		return active, nil, false
	}
	if attempt := r.dials[key]; attempt != nil {
		return nil, attempt, false
	}
	attempt := &dialAttempt{done: make(chan struct{})}
	r.dials[key] = attempt
	return nil, attempt, true
}

func (r *connectionRegistry) finishDial(key string, attempt *dialAttempt, active *session, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	attempt.session = active
	attempt.err = err
	delete(r.dials, key)
	close(attempt.done)
}

func (r *connectionRegistry) cacheSession(active *session, identities ...[]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range identities {
		if len(value) == 16 {
			r.sessions[hex.EncodeToString(value)] = active
		}
	}
}

func (r *connectionRegistry) rememberDestination(identity, destinationHash []byte) {
	if len(identity) != 16 || len(destinationHash) != 16 {
		return
	}
	r.mu.Lock()
	r.destinations[hex.EncodeToString(identity)] = bytes.Clone(destinationHash)
	r.mu.Unlock()
}

func (r *connectionRegistry) addWaiter(key string) chan struct{} {
	waiter := make(chan struct{})
	r.mu.Lock()
	r.waiters[key] = append(r.waiters[key], waiter)
	r.mu.Unlock()
	return waiter
}

func (r *connectionRegistry) removeWaiter(key string, waiter chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.waiters[key]
	for index, entry := range entries {
		if entry == waiter {
			entries = append(entries[:index], entries[index+1:]...)
			break
		}
	}
	if len(entries) == 0 {
		delete(r.waiters, key)
	} else {
		r.waiters[key] = entries
	}
}

func (r *connectionRegistry) takeWaiters(key string) []chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiters := r.waiters[key]
	delete(r.waiters, key)
	return waiters
}

func (r *connectionRegistry) allSessions() []*session {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[*session]struct{}, len(r.sessions))
	for _, active := range r.sessions {
		seen[active] = struct{}{}
	}
	result := make([]*session, 0, len(seen))
	for active := range seen {
		result = append(result, active)
	}
	return result
}
