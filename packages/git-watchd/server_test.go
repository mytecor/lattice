package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mytecor/meshbus/core"
)

type memoryNetwork struct {
	mu        sync.RWMutex
	endpoints map[core.PeerID]*memoryNodeTransport
}

func newMemoryNetwork() *memoryNetwork {
	return &memoryNetwork{endpoints: make(map[core.PeerID]*memoryNodeTransport)}
}

type memoryNodeTransport struct {
	network  *memoryNetwork
	id       core.PeerID
	handler  core.Handler
	observer core.PeerObserver

	mu      sync.RWMutex
	started bool
	closed  bool
}

func (n *memoryNetwork) factory(identity byte, capture **memoryNodeTransport) core.TransportFactory {
	return func(handler core.Handler) (core.NodeTransport, error) {
		peer, err := core.NewPeerID([]byte{identity})
		if err != nil {
			return nil, err
		}
		transport := &memoryNodeTransport{network: n, id: peer, handler: handler}
		n.mu.Lock()
		n.endpoints[transport.id] = transport
		n.mu.Unlock()
		*capture = transport
		return transport, nil
	}
}

func (t *memoryNodeTransport) Start(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("transport closed")
	}
	t.started = true
	return nil
}

func (t *memoryNodeTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	t.network.mu.Lock()
	delete(t.network.endpoints, t.id)
	t.network.mu.Unlock()
	return nil
}

func (t *memoryNodeTransport) Identity() core.PeerID { return t.id }

func (t *memoryNodeTransport) SetPeerObserver(observer core.PeerObserver) error {
	t.observer = observer
	return nil
}

func (t *memoryNodeTransport) SendMessage(ctx context.Context, peer core.PeerID, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.RLock()
	ready := t.started && !t.closed
	t.mu.RUnlock()
	if !ready {
		return errors.New("memory transport not ready")
	}
	t.network.mu.RLock()
	target := t.network.endpoints[peer]
	t.network.mu.RUnlock()
	if target == nil {
		return errors.New("peer unavailable")
	}
	target.mu.RLock()
	targetReady := target.started && !target.closed
	handler := target.handler
	target.mu.RUnlock()
	if !targetReady {
		return errors.New("target not ready")
	}
	if err := t.observer.Authenticated(target.id); err != nil {
		return err
	}
	if err := target.observer.Authenticated(t.id); err != nil {
		return err
	}
	msg, err := core.NewReceivedMessage(t.id.Bytes(), payload)
	if err != nil {
		return err
	}
	return handler(ctx, msg)
}

func setupTestMeshbusPair(t *testing.T) (*core.Node, *core.Node) {
	t.Helper()
	net := newMemoryNetwork()

	var tServer, tClient *memoryNodeTransport
	serverNode, err := core.NewNode(core.NodeConfig{
		Transport: net.factory(0x01, &tServer),
	})
	if err != nil {
		t.Fatal(err)
	}

	clientNode, err := core.NewNode(core.NodeConfig{
		Transport: net.factory(0x02, &tClient),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = clientNode.Close()
		_ = serverNode.Close()
	})

	if err := serverNode.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := clientNode.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Direct handshake to authenticate peers
	if err := tServer.observer.Authenticated(clientNode.Identity()); err != nil {
		t.Fatal(err)
	}
	if err := tClient.observer.Authenticated(serverNode.Identity()); err != nil {
		t.Fatal(err)
	}

	return serverNode, clientNode
}

func TestServerAndClientRPC(t *testing.T) {
	serverNode, clientNode := setupTestMeshbusPair(t)
	dir := setupTestGitRepo(t)

	engine := NewLocalGitEngine("", "")
	watcher := NewWatcher(engine, 50*time.Millisecond)

	server, err := NewServer(ServerConfig{
		Bus:     serverNode,
		Engine:  engine,
		Watcher: watcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}

	client := NewClient(clientNode, "", "")

	// 1. Head request
	headResp, err := client.Head(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("client.Head failed: %v", err)
	}
	if len(headResp.Revision) != 40 {
		t.Errorf("expected 40-char commit SHA, got %q", headResp.Revision)
	}
	rev1 := headResp.Revision

	// 2. Head request for non-existent repo
	_, err = client.Head(ctx, "/tmp/non-existent-repo-12345", "HEAD")
	if err == nil {
		t.Error("expected error for non-existent repo, got nil")
	}

	// 3. Head request for non-existent ref
	_, err = client.Head(ctx, dir, "refs/heads/invalid-ref-12345")
	if err == nil {
		t.Error("expected error for invalid ref, got nil")
	}

	// 4. Initial Watch (empty after_revision)
	watchResp1, err := client.Watch(ctx, dir, "HEAD", "", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("client.Watch initial failed: %v", err)
	}
	if watchResp1.Status != StatusChanged {
		t.Errorf("expected StatusChanged, got %s", watchResp1.Status)
	}
	if watchResp1.To != rev1 {
		t.Errorf("expected To == %s, got %s", rev1, watchResp1.To)
	}
	if len(watchResp1.ChangedPaths) != 1 || watchResp1.ChangedPaths[0] != "README.md" {
		t.Errorf("expected [README.md], got %v", watchResp1.ChangedPaths)
	}

	// 5. Watch behind (cursor is rev1, commit 2 added)
	addCommit(t, dir, "task.md", "---\nid: f11\n---\n", "add task")
	headResp2, err := client.Head(ctx, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	rev2 := headResp2.Revision

	watchResp2, err := client.Watch(ctx, dir, "HEAD", rev1, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("client.Watch behind failed: %v", err)
	}
	if watchResp2.Status != StatusChanged {
		t.Errorf("expected StatusChanged, got %s", watchResp2.Status)
	}
	if watchResp2.From != rev1 || watchResp2.To != rev2 {
		t.Errorf("expected from %s to %s, got %s -> %s", rev1, rev2, watchResp2.From, watchResp2.To)
	}
	if len(watchResp2.ChangedPaths) != 1 || watchResp2.ChangedPaths[0] != "task.md" {
		t.Errorf("expected [task.md], got %v", watchResp2.ChangedPaths)
	}

	// 6. Watch at head with timeout (no changes) -> StatusNoChange
	watchResp3, err := client.Watch(ctx, dir, "HEAD", rev2, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("client.Watch no_change failed: %v", err)
	}
	if watchResp3.Status != StatusNoChange {
		t.Errorf("expected StatusNoChange, got %s", watchResp3.Status)
	}
	if watchResp3.Revision != rev2 {
		t.Errorf("expected Revision == %s, got %s", rev2, watchResp3.Revision)
	}

	// 7. Watch at head with change during long-poll
	watchDoneCh := make(chan *WatchResponse, 1)
	watchErrCh := make(chan error, 1)
	go func() {
		resp, err := client.Watch(ctx, dir, "HEAD", rev2, 2*time.Second)
		if err != nil {
			watchErrCh <- err
		} else {
			watchDoneCh <- resp
		}
	}()

	time.Sleep(100 * time.Millisecond)
	addCommit(t, dir, "result.json", "{\"status\": \"ok\"}", "add result")
	headResp3, err := client.Head(ctx, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	rev3 := headResp3.Revision

	select {
	case err := <-watchErrCh:
		t.Fatalf("client.Watch long poll failed: %v", err)
	case resp := <-watchDoneCh:
		if resp.Status != StatusChanged {
			t.Errorf("expected StatusChanged, got %s", resp.Status)
		}
		if resp.From != rev2 || resp.To != rev3 {
			t.Errorf("expected from %s to %s, got %s -> %s", rev2, rev3, resp.From, resp.To)
		}
		if len(resp.ChangedPaths) != 1 || resp.ChangedPaths[0] != "result.json" {
			t.Errorf("expected [result.json], got %v", resp.ChangedPaths)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for client.Watch response")
	}
}
