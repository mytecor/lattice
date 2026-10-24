package acp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

func TestWebSocketProtocolAndNoTransportReplay(t *testing.T) {
	var turns atomic.Int32
	var dials atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{Subprotocols: []string{"acp.v1"}}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req struct {
				ID     uint64 `json:"id"`
				Method string `json:"method"`
			}
			if ws.ReadJSON(&req) != nil {
				return
			}
			var result any
			switch req.Method {
			case "initialize":
				result = map[string]int{"protocolVersion": 1}
			case "session/new":
				result = map[string]string{"sessionId": "fresh"}
			case "session/prompt":
				if turns.Add(1) == 2 {
					return
				} // ordinary transport loss must never replay a turn
				_ = ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fresh"}})
				_ = ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": "permission", "method": "session/request_permission", "params": map[string]any{"options": []any{map[string]string{"optionId": "always", "kind": "allow_always"}, map[string]string{"optionId": "once", "kind": "allow_once"}}}})
				var reply struct {
					Result struct {
						Outcome struct{ Outcome, OptionID string } `json:"outcome"`
					} `json:"result"`
				}
				if err := ws.ReadJSON(&reply); err != nil {
					t.Error(err)
					return
				}
				if reply.Result.Outcome.Outcome != "selected" || reply.Result.Outcome.OptionID != "once" {
					b, _ := json.Marshal(reply)
					t.Errorf("permission not limited to one use: %s", b)
				}
				result = map[string]string{"stopReason": "end_turn"}
			}
			_ = ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := &WebSocketClient{Dial: func(ctx context.Context, _ string) (Transport, error) {
		if dials.Add(1) == 1 {
			local, remote := net.Pipe()
			_ = remote.Close()
			return Transport{Conn: local, Token: "test-token"}, nil
		}
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(server.URL, "http://"))
		return Transport{Conn: c, Token: "test-token"}, err
	}}
	session, err := client.Connect(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err = session.Initialize(ctx, &taskcontract.TaskSpec{}); err != nil {
		t.Fatal(err)
	}
	result, err := session.Prompt(ctx, "first")
	if err != nil || result.Status != TurnCompleted {
		t.Fatalf("turn: %v, %v", result, err)
	}
	if _, err = session.Prompt(ctx, "second"); err == nil {
		t.Fatal("transport error hidden")
	}
	if dials.Load() != 2 {
		t.Fatal("bootstrap readiness did not retry only the handshake")
	}
	if turns.Load() != 2 {
		t.Fatal("ordinary transport loss replayed prompt")
	}
}
