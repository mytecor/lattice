package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mytecor/lattice/agentd/internal/acp"
	taskcontract "github.com/mytecor/lattice/task-contract"
	r1sv1 "github.com/mytecor/r1s/api/gen/r1s/v1"
	r1s "github.com/mytecor/r1s/client"
)

type fakeRunClient struct {
	events  chan r1s.Event
	request chan *r1sv1.ExecutionRequest
	closed  atomic.Bool
}

func (c *fakeRunClient) Start(context.Context) error { return nil }
func (c *fakeRunClient) Close() []r1s.PendingRelease { c.closed.Store(true); return nil }
func (c *fakeRunClient) Run(ctx context.Context, req *r1sv1.ExecutionRequest, opts r1s.RunOptions) (r1s.Result, error) {
	c.request <- req
	for {
		select {
		case <-ctx.Done():
			return r1s.Result{}, ctx.Err()
		case e := <-c.events:
			opts.OnEvent(e)
		}
	}
}

type fakeRunTunnel struct {
	address string
	mu      sync.Mutex
	streams []r1s.Stream
	active  string
	closed  atomic.Bool
	calls   atomic.Int32
}

func (t *fakeRunTunnel) SetActive(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active != id {
		for _, s := range t.streams {
			_ = s.Close()
		}
		t.streams = nil
	}
	t.active = id
}
func (t *fakeRunTunnel) Dial(ctx context.Context, port uint16) (r1s.Stream, error) {
	t.calls.Add(1)
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", t.address)
	if err != nil {
		return nil, err
	}
	stream := c.(*net.TCPConn)
	t.mu.Lock()
	t.streams = append(t.streams, stream)
	t.mu.Unlock()
	return stream, nil
}
func (t *fakeRunTunnel) Close() error { t.SetActive(""); t.closed.Store(true); return nil }
func testRemote(t *testing.T, address string) (*R1SRuntime, *fakeRunClient, *fakeRunTunnel) {
	t.Helper()
	r, err := NewR1SRuntime(R1SConfig{Client: r1s.Config{ClusterKey: make([]byte, 32)}, Environment: map[string]string{"SAFE": "value", "SOURCE_REPO": "must-be-overridden"}})
	if err != nil {
		t.Fatal(err)
	}
	c := &fakeRunClient{events: make(chan r1s.Event, 8), request: make(chan *r1sv1.ExecutionRequest, 1)}
	tunnel := &fakeRunTunnel{address: address}
	r.create = func() (runClient, runTunnel, error) { return c, tunnel, nil }
	return r, c, tunnel
}
func testSpec() *taskcontract.TaskSpec {
	return &taskcontract.TaskSpec{ID: "task", Source: taskcontract.SourceSpec{Repo: "https://example.org/source.git", Revision: strings.Repeat("a", 40)}, Runtime: taskcontract.RuntimeSpec{Image: "example/image@sha256:" + strings.Repeat("b", 64)}, Context: "task context", Requirements: "make the requested change"}
}

// This crosses the real WebSocket/JSON-RPC boundary through the exact stream
// adapter used by r1s. Only placement and the allocator tunnel are substituted.
func TestR1SACPReassignment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var sessions, prompts atomic.Int32
	firstPrompt := make(chan struct{})
	var tokens sync.Map
	var newPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocols := websocket.Subprotocols(r)
		if len(protocols) != 2 || !strings.HasPrefix(protocols[1], "hydra-acp-token.") {
			t.Error("missing scoped ACP token")
			http.Error(w, "auth", 401)
			return
		}
		tokens.Store(protocols[1], true)
		ws, err := (&websocket.Upgrader{Subprotocols: []string{"acp.v1"}}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req struct {
				ID     uint64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if ws.ReadJSON(&req) != nil {
				return
			}
			var result any
			switch req.Method {
			case "initialize":
				result = map[string]any{"protocolVersion": 1}
			case "session/new":
				n := sessions.Add(1)
				result = map[string]string{"sessionId": string(rune('a' + n))}
			case "session/prompt":
				n := prompts.Add(1)
				if n == 1 {
					close(firstPrompt)
					var ignored any
					_ = ws.ReadJSON(&ignored)
					return
				}
				newPrompt = string(req.Params)
				result = map[string]string{"stopReason": "end_turn"}
			default:
				t.Errorf("unexpected method %s", req.Method)
				return
			}
			if ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) != nil {
				return
			}
		}
	}))
	defer server.Close()
	r, c, tunnel := testRemote(t, strings.TrimPrefix(server.URL, "http://"))
	inst, err := r.Start(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Stop(context.Background())
	request := <-c.request
	if request.GetWorkload().GetImage() != testSpec().Runtime.Image || request.ResourceClass != "default" {
		t.Fatal("incorrect workload request")
	}
	env := request.Workload.Environment
	if env["SOURCE_REPO"] != testSpec().Source.Repo || env["SOURCE_REVISION"] != testSpec().Source.Revision || env["ACP_PORT"] != "55514" || len(env["ACP_SECRET"]) != 64 || env["SAFE"] != "value" {
		t.Fatal("incorrect container bootstrap contract")
	}
	if strings.Contains(inst.Endpoint(), env["ACP_SECRET"]) {
		t.Fatal("endpoint exposes token")
	}
	c.events <- r1s.Event{Kind: r1s.EventAttemptAssigned, Attempt: r1s.Attempt{ExecutionID: "first"}}
	client := &acp.WebSocketClient{Dial: r.DialACP}
	session, err := client.Connect(ctx, inst.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err = session.Initialize(ctx, testSpec()); err != nil {
		t.Fatal(err)
	}
	go func() {
		select {
		case <-firstPrompt:
			c.events <- r1s.Event{Kind: r1s.EventAttemptAssigned, Attempt: r1s.Attempt{ExecutionID: "second"}}
		case <-ctx.Done():
		}
	}()
	result, err := session.Prompt(ctx, "do the task")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != acp.TurnCompleted || sessions.Load() != 2 || prompts.Load() != 2 {
		t.Fatal("reassignment did not recreate session and replay task")
	}
	if !strings.Contains(newPrompt, "task context") || !strings.Contains(newPrompt, "do the task") {
		t.Fatal("replacement session lost task context")
	}
	if err = inst.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.closed.Load() || !tunnel.closed.Load() {
		t.Fatal("run resources leaked")
	}
	if _, err = r.DialACP(ctx, inst.Endpoint()); err == nil {
		t.Fatal("stopped endpoint still usable")
	}
}

func TestR1SCancelBeforeAssignment(t *testing.T) {
	r, c, tunnel := testRemote(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	inst, err := r.Start(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = r.DialACP(ctx, inst.Endpoint()); err == nil {
		t.Fatal("cancellation ignored")
	}
	stopCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = inst.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if !c.closed.Load() || !tunnel.closed.Load() {
		t.Fatal("canceled run leaked resources")
	}
}
func TestR1SRequiresDeclaredInputs(t *testing.T) {
	r, _, _ := testRemote(t, "")
	for _, spec := range []*taskcontract.TaskSpec{nil, {}, {Source: testSpec().Source}} {
		if _, err := r.Start(context.Background(), spec); err == nil {
			t.Fatal("missing input accepted")
		}
	}
	_, err := NewR1SRuntime(R1SConfig{})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("missing cluster authority accepted")
	}
}
