package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// Transport is one execution's byte stream. Changed closes only when that
// execution is replaced/lost; ordinary transport failures must not replay turns.
type Transport struct {
	Conn    net.Conn
	Changed <-chan struct{}
	Token   string
}
type DialTransport func(context.Context, string) (Transport, error)

type WebSocketClient struct {
	Dial DialTransport
	Cwd  string
}

func (c *WebSocketClient) Connect(ctx context.Context, endpoint string) (Session, error) {
	if c.Dial == nil {
		return nil, errors.New("ACP transport dialer is required")
	}
	cwd := c.Cwd
	if cwd == "" {
		cwd = "/workspace"
	}
	return &webSocketSession{client: c, endpoint: endpoint, cwd: cwd, lifetime: ctx}, nil
}

type webSocketSession struct {
	mu                       sync.Mutex
	client                   *WebSocketClient
	endpoint, cwd, sessionID string
	lifetime                 context.Context
	spec                     *taskcontract.TaskSpec
	conn                     *websocket.Conn
	changed                  <-chan struct{}
	stopWatch                context.CancelFunc
	nextID                   uint64
	lastMessage              string
	closed                   bool
}

func (s *webSocketSession) Initialize(ctx context.Context, spec *taskcontract.TaskSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("ACP session is closed")
	}
	if spec == nil {
		return errors.New("TaskSpec is required")
	}
	s.spec = spec
	return s.open(ctx)
}

func (s *webSocketSession) disconnect() {
	if s.stopWatch != nil {
		s.stopWatch()
		s.stopWatch = nil
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	s.sessionID = ""
}

func changed(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// open always creates a fresh ACP session. It never attaches to an old execution.
func (s *webSocketSession) open(ctx context.Context) error {
	s.disconnect()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tr, err := s.client.Dial(ctx, s.endpoint)
		if err != nil {
			return err
		}
		used := false
		d := websocket.Dialer{HandshakeTimeout: 10 * time.Second,
			Subprotocols: []string{"acp.v1", "hydra-acp-token." + tr.Token},
			NetDialContext: func(context.Context, string, string) (net.Conn, error) {
				if used {
					return nil, errors.New("ACP transport already consumed")
				}
				used = true
				return tr.Conn, nil
			},
		}
		conn, resp, err := d.DialContext(ctx, "ws://agent/acp", http.Header{})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			_ = tr.Conn.Close()
			if changed(tr.Changed) {
				continue
			}
			// r1s can establish a stream before container bootstrap has opened
			// its ACP listener; the allocator then closes it asynchronously.
			// No turn has been sent, so retrying this handshake is safe.
			if resp == nil || resp.StatusCode == 502 || resp.StatusCode == 503 {
				timer := time.NewTimer(200 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return fmt.Errorf("ACP WebSocket handshake: %w", err)
		}
		conn.SetReadLimit(8 << 20)
		s.conn, s.changed = conn, tr.Changed
		watchCtx, cancel := context.WithCancel(s.lifetime)
		s.stopWatch = cancel
		go func() {
			select {
			case <-watchCtx.Done():
			case <-tr.Changed:
			}
			_ = conn.Close()
		}()
		var init struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		err = s.call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]string{"name": "lattice-agentd", "version": "0.3.0"}}, &init)
		if err == nil && init.ProtocolVersion != 1 {
			err = errors.New("unsupported ACP protocol version")
		}
		if err == nil {
			var session struct {
				SessionID string `json:"sessionId"`
			}
			err = s.call(ctx, "session/new", map[string]any{"cwd": s.cwd, "mcpServers": []any{}}, &session)
			if err == nil && session.SessionID == "" {
				err = errors.New("ACP session/new returned no sessionId")
			}
			s.sessionID = session.SessionID
		}
		if err == nil {
			return nil
		}
		replaced := changed(s.changed)
		s.disconnect()
		if !replaced {
			return err
		}
	}
}

func (s *webSocketSession) Prompt(ctx context.Context, prompt string) (*TurnResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.spec == nil {
		return nil, errors.New("ACP session is not initialized")
	}
	for {
		if changed(s.changed) {
			if err := s.open(ctx); err != nil {
				return nil, err
			}
			// f19-02 restores task context only. Git/verifier recovery belongs to f19-03.
			data, _ := json.Marshal(s.spec)
			prompt = "Execution replaced. Start from this task specification:\n" + string(data) + "\n" + prompt
		}
		s.lastMessage = ""
		var result struct {
			StopReason string `json:"stopReason"`
		}
		err := s.call(ctx, "session/prompt", map[string]any{"sessionId": s.sessionID, "prompt": []any{map[string]string{"type": "text", "text": prompt}}}, &result)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if changed(s.changed) {
				continue
			}
			return nil, err
		}
		if changed(s.changed) {
			continue
		}
		if result.StopReason != "end_turn" {
			return &TurnResult{Status: TurnFailed, Message: "ACP stop reason: " + result.StopReason}, nil
		}
		return &TurnResult{Status: TurnCompleted, Message: s.lastMessage}, nil
	}
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *webSocketSession) call(ctx context.Context, method string, params, result any) error {
	if s.conn == nil {
		return errors.New("ACP connection is closed")
	}
	conn := s.conn
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
		_ = conn.SetWriteDeadline(deadline)
	}
	s.nextID++
	id := s.nextID
	if err := conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var msg rpcMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		if msg.JSONRPC != "2.0" {
			return errors.New("invalid ACP JSON-RPC version")
		}
		if msg.Method != "" {
			if len(msg.ID) == 0 {
				if msg.Method == "session/update" {
					var update struct {
						SessionID string `json:"sessionId"`
						Update    struct {
							Kind    string `json:"sessionUpdate"`
							Content struct {
								Type string `json:"type"`
								Text string `json:"text"`
							} `json:"content"`
						} `json:"update"`
					}
					if json.Unmarshal(msg.Params, &update) == nil && update.SessionID == s.sessionID && update.Update.Kind == "agent_message_chunk" && update.Update.Content.Type == "text" {
						remaining := 4096 - len(s.lastMessage)
						if remaining > 0 {
							chunk := update.Update.Content.Text
							if len(chunk) > remaining {
								chunk = chunk[:remaining]
							}
							s.lastMessage += chunk
						}
					}
				}
				continue
			} // session/update and other notifications
			// No host filesystem/terminal capabilities are advertised. Unattended
			// execution may grant one tool use, never a persistent permission.
			response := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
			if msg.Method == "session/request_permission" {
				var p struct {
					Options []struct {
						OptionID string `json:"optionId"`
						Kind     string `json:"kind"`
					} `json:"options"`
				}
				if err := json.Unmarshal(msg.Params, &p); err != nil {
					return errors.New("invalid ACP permission request")
				}
				outcome := map[string]string{"outcome": "cancelled"}
				for _, o := range p.Options {
					if o.Kind == "allow_once" {
						outcome = map[string]string{"outcome": "selected", "optionId": o.OptionID}
						break
					}
				}
				response["result"] = map[string]any{"outcome": outcome}
			} else {
				response["error"] = map[string]any{"code": -32601, "message": "client capability is not available"}
			}
			if err := conn.WriteJSON(response); err != nil {
				return err
			}
			continue
		}
		var got uint64
		if json.Unmarshal(msg.ID, &got) != nil || got != id {
			return errors.New("unexpected ACP response ID")
		}
		if msg.Error != nil {
			return fmt.Errorf("ACP %s failed (code %d)", method, msg.Error.Code)
		}
		if len(msg.Result) == 0 {
			return errors.New("ACP response has no result")
		}
		return json.Unmarshal(msg.Result, result)
	}
}

func (s *webSocketSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.disconnect()
	return nil
}
