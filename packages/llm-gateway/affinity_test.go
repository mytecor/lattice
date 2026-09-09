package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAffinityStoreBindLookupExpire(t *testing.T) {
	now := time.Unix(2000, 0)
	store := newAffinityStore(func() time.Time { return now }, "")
	store.Bind("resp_1", "a", time.Hour)
	if provider, ok := store.Lookup("resp_1"); !ok || provider != "a" {
		t.Fatalf("mapping not found: %q %v", provider, ok)
	}
	now = now.Add(2 * time.Hour)
	if _, ok := store.Lookup("resp_1"); ok {
		t.Fatal("expired mapping still returned")
	}
	now = time.Unix(2000, 0)
	store.Bind("resp_2", "a", time.Hour)
	store.Bind("resp_2", "b", time.Hour) // rebind extends/moves
	if provider, ok := store.Lookup("resp_2"); !ok || provider != "b" {
		t.Fatalf("rebind did not update provider: %q %v", provider, ok)
	}
}

func TestAffinityStorePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "affinity.json")
	now := time.Unix(3000, 0)
	first := newAffinityStore(func() time.Time { return now }, path)
	first.Bind("stateful_id", "hyperfusion", time.Hour)
	first.Bind("other_id", "dahl", time.Hour)
	first.Bind("forgotten_id", "retired-provider", time.Hour)
	first.Forget("forgotten_id")
	if first.Count() != 2 {
		t.Fatalf("unexpected count after binds: %d", first.Count())
	}
	// Persistence is batched by the background flusher; Close flushes before
	// the new process reads the file.
	first.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("affinity file was not written")
	}
	// A new process with a later clock must still see live mappings and drop
	// expired ones.
	second := newAffinityStore(func() time.Time { return now.Add(30 * time.Minute) }, path)
	defer second.Close()
	if provider, ok := second.Lookup("stateful_id"); !ok || provider != "hyperfusion" {
		t.Fatalf("mapping did not survive restart: %q %v", provider, ok)
	}
	if _, ok := second.Lookup("forgotten_id"); ok {
		t.Fatal("forgotten mapping reappeared after restart")
	}
	third := newAffinityStore(func() time.Time { return now.Add(2 * time.Hour) }, path)
	defer third.Close()
	if _, ok := third.Lookup("stateful_id"); ok {
		t.Fatal("mapping persisted past its TTL")
	}
}

func TestAffinityStoreFileHas0600Mode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "affinity.json")
	store := newAffinityStore(time.Now, path)
	store.Bind("id", "a", time.Hour)
	store.Close() // flush to disk before stat
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("affinity file mode is %o, want 600", mode)
	}
}

func TestAffinityStoreRetriesFailedFlush(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	path := filepath.Join(dir, "affinity.json")
	reported := 0
	store := newAffinityStore(time.Now, path, func(error) { reported++ })
	store.Bind("resp_1", "a", time.Hour)
	if err := store.flushSnapshot(); err == nil {
		t.Fatal("flush to a missing directory unexpectedly succeeded")
	}
	if reported != 1 {
		t.Fatalf("flush failure was not reported: %d", reported)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.flushSnapshot(); err != nil {
		t.Fatalf("dirty snapshot was not retried: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("successful retry left a stale close error: %v", err)
	}
	reloaded := newAffinityStore(time.Now, path)
	defer reloaded.Close()
	if provider, ok := reloaded.Lookup("resp_1"); !ok || provider != "a" {
		t.Fatalf("retried snapshot did not persist mapping: %q %v", provider, ok)
	}
}

func TestAffinityStoreCloseReturnsFinalFlushError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "affinity.json")
	reported := make(chan error, 1)
	store := newAffinityStore(time.Now, path, func(err error) { reported <- err })
	store.Bind("resp_1", "a", time.Hour)
	if err := store.Close(); err == nil {
		t.Fatal("Close hid the final affinity flush failure")
	}
	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("affinity reporter received a nil error")
		}
	default:
		t.Fatal("final affinity flush failure was not reported")
	}
}

func TestConversationIDExtraction(t *testing.T) {
	if got := conversationID([]byte(`"conv_1"`)); got != "conv_1" {
		t.Fatalf("string conversation not extracted: %q", got)
	}
	if got := conversationID([]byte(`{"id":"conv_2"}`)); got != "conv_2" {
		t.Fatalf("object conversation not extracted: %q", got)
	}
	if got := conversationID([]byte(`null`)); got != "" {
		t.Fatalf("null conversation produced an id: %q", got)
	}
}

func TestRequestAffinityIDSourcesAndChatIgnored(t *testing.T) {
	body := []byte(`{"model":"standard","conversation":{"id":"conv_1"},"previous_response_id":"resp_9","input":"hi"}`)
	if got := requestAffinityID(body, RequestResponses, []string{"responses.previous_response_id"}); got != "resp_9" {
		t.Fatalf("previous_response_id not extracted: %q", got)
	}
	if got := requestAffinityID(body, RequestResponses, []string{"responses.conversation", "responses.previous_response_id"}); got != "conv_1" {
		t.Fatalf("conversation has priority over previous_response_id: %q", got)
	}
	if got := requestAffinityID([]byte(`{"model":"standard","conversation":"conv_1","messages":[]}`), RequestChat, []string{"responses.conversation"}); got != "" {
		t.Fatalf("chat request produced an affinity key: %q", got)
	}
}

func TestResponseBodyAndStreamAffinityIDs(t *testing.T) {
	body := []byte(`{"id":"resp_1","conversation":{"id":"conv_1"},"object":"response"}`)
	ids := responseBodyAffinityIDs(body)
	if len(ids) != 2 || ids[0] != "resp_1" || ids[1] != "conv_1" {
		t.Fatalf("response body ids not extracted: %#v", ids)
	}
	chunk := []byte(`{"type":"response.created","response":{"id":"resp_1","conversation":{"id":"conv_1"}}}`)
	if got := streamResponseAffinityIDs(chunk, "response.created"); len(got) != 2 || got[0] != "resp_1" || got[1] != "conv_1" {
		t.Fatalf("stream response ids not extracted: %#v", got)
	}
	delta := []byte(`{"type":"response.output_text.delta","delta":"hi"}`)
	if got := streamResponseAffinityIDs(delta, "response.output_text.delta"); len(got) != 0 {
		t.Fatalf("delta event produced response ids: %#v", got)
	}
}
