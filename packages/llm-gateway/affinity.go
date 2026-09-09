package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// affinityFlushInterval bounds how long a mapping change can stay unsaved. The
// file is rewritten only while dirty, so quiet traffic causes no disk writes
// and a burst of responses produces at most one write per interval.
const affinityFlushInterval = 5 * time.Second

// AffinityStore binds opaque Responses state identifiers (response ids,
// conversation ids, previous_response_id) to the provider that produced them.
// It stores only the opaque id → provider mapping plus an expiry; prompts,
// request bodies, keys, and provider URLs are never persisted. When a path is
// configured the mapping is snapshotted to a file owned by the gateway user so
// it survives service restarts.
type AffinityStore struct {
	mu            sync.Mutex
	entries       map[string]affinityEntry
	now           func() time.Time
	path          string
	dirty         bool
	generation    uint64
	lastFlushErr  error
	reportError   func(error)
	stop          chan struct{}
	done          chan struct{}
	closeOnce     sync.Once
	flushInterval time.Duration
}

type affinityEntry struct {
	Provider  string `json:"provider"`
	ExpiresAt int64  `json:"expires_at"` // unix nanoseconds
}

func newAffinityStore(now func() time.Time, path string, reporters ...func(error)) *AffinityStore {
	if now == nil {
		now = time.Now
	}
	store := &AffinityStore{
		entries:       make(map[string]affinityEntry),
		now:           now,
		path:          path,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		flushInterval: affinityFlushInterval,
	}
	if len(reporters) > 0 {
		store.reportError = reporters[0]
	}
	if path != "" {
		store.load()
		go store.runFlusher()
	}
	return store
}

// Lookup returns the provider pinned to the opaque id if the mapping exists and
// has not expired.
func (s *AffinityStore) Lookup(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return "", false
	}
	if s.now().UnixNano() >= entry.ExpiresAt {
		delete(s.entries, id)
		s.dirty = true
		s.generation++
		return "", false
	}
	return entry.Provider, true
}

// Bind records (or extends) the opaque id → provider mapping with the given TTL.
// The mapping is applied in memory immediately; persistence is batched by the
// background flusher so the request path never performs disk I/O.
func (s *AffinityStore) Bind(id, provider string, ttl time.Duration) {
	if id == "" || provider == "" || ttl <= 0 {
		return
	}
	s.mu.Lock()
	s.entries[id] = affinityEntry{Provider: provider, ExpiresAt: s.now().Add(ttl).UnixNano()}
	s.dirty = true
	s.generation++
	s.mu.Unlock()
}

// Forget drops a mapping. It is used when the bound provider no longer exists
// in the config so the request falls back to on_missing behavior instead of
// failing closed for the remaining TTL.
func (s *AffinityStore) Forget(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	if _, exists := s.entries[id]; exists {
		delete(s.entries, id)
		s.dirty = true
		s.generation++
	}
	s.mu.Unlock()
}

// Count reports the number of live mappings (for tests and metrics).
func (s *AffinityStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UnixNano()
	count := 0
	removed := false
	for id, entry := range s.entries {
		if now < entry.ExpiresAt {
			count++
		} else {
			delete(s.entries, id)
			removed = true
		}
	}
	if removed {
		s.dirty = true
		s.generation++
	}
	return count
}

// flushSnapshot persists the live mappings when the store is dirty. Expired
// entries are compacted here, so the file stays bounded by the number of live
// mappings rather than by cumulative traffic. The write happens outside the
// lock to keep marshaling and disk I/O off the request path.
func (s *AffinityStore) flushSnapshot() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	generation := s.generation
	now := s.now().UnixNano()
	snapshot := make(map[string]affinityEntry, len(s.entries))
	for id, entry := range s.entries {
		if now < entry.ExpiresAt {
			snapshot[id] = entry
		}
	}
	s.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return s.finishFlush(generation, fmt.Errorf("encode affinity snapshot: %w", err))
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return s.finishFlush(generation, fmt.Errorf("write affinity snapshot: %w", err))
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return s.finishFlush(generation, fmt.Errorf("replace affinity snapshot: %w", err))
	}
	return s.finishFlush(generation, nil)
}

func (s *AffinityStore) finishFlush(generation uint64, flushErr error) error {
	s.mu.Lock()
	if flushErr == nil {
		if s.generation == generation {
			s.dirty = false
		}
		s.lastFlushErr = nil
	} else {
		// Preserve dirty state so the next periodic or shutdown flush retries.
		s.dirty = true
		s.lastFlushErr = flushErr
	}
	report := s.reportError
	s.mu.Unlock()
	if flushErr != nil && report != nil {
		report(flushErr)
	}
	return flushErr
}

// runFlusher periodically persists dirty mappings and performs a final flush
// when Close signals the stop channel.
func (s *AffinityStore) runFlusher() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			_ = s.flushSnapshot()
			return
		case <-ticker.C:
			_ = s.flushSnapshot()
		}
	}
}

// Close stops the background flusher and persists any remaining dirty mappings
// so a graceful shutdown does not lose recent bindings.
func (s *AffinityStore) Close() error {
	if s.path == "" {
		return nil
	}
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.done
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastFlushErr
}

func (s *AffinityStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.entries)
}

// conversationID extracts a conversation identifier from either a plain string
// or an object with an "id" field.
func conversationID(raw []byte) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if strings.HasPrefix(trimmed, `"`) {
		var value string
		if json.Unmarshal([]byte(trimmed), &value) == nil {
			return value
		}
		return ""
	}
	var object struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(trimmed), &object) == nil {
		return object.ID
	}
	return ""
}

// requestAffinityID extracts the first known stateful identifier from a
// Responses request body according to the configured sources. Chat requests
// never produce an affinity key: the gateway does not hash messages and does
// not treat prompt_cache_key as a session identifier.
func requestAffinityID(body []byte, kind RequestKind, sources []string) string {
	if kind != RequestResponses || len(sources) == 0 {
		return ""
	}
	var request struct {
		Conversation       json.RawMessage `json:"conversation"`
		PreviousResponseID string          `json:"previous_response_id"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}
	for _, source := range sources {
		switch source {
		case "responses.conversation":
			if id := conversationID(request.Conversation); id != "" {
				return id
			}
		case "responses.previous_response_id":
			if request.PreviousResponseID != "" {
				return request.PreviousResponseID
			}
		}
	}
	return ""
}

// responseBodyAffinityIDs extracts the response id and conversation identifier
// from a non-streaming Responses response body.
func responseBodyAffinityIDs(data []byte) []string {
	var response struct {
		ID           string          `json:"id"`
		Conversation json.RawMessage `json:"conversation"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil
	}
	ids := make([]string, 0, 2)
	if response.ID != "" {
		ids = append(ids, response.ID)
	}
	if id := conversationID(response.Conversation); id != "" {
		ids = append(ids, id)
	}
	return ids
}

// streamResponseAffinityIDs extracts the response and conversation identifiers
// from a streaming Responses chunk. Only response.created / response.completed
// events carry a full response object; delta events are ignored.
func streamResponseAffinityIDs(data []byte, eventType string) []string {
	if eventType != "response.created" && eventType != "response.completed" {
		return nil
	}
	var chunk struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil
	}
	return responseBodyAffinityIDs(chunk.Response)
}
