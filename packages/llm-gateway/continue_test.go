package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAccumulatePartialCollectsContentAndReasoning(t *testing.T) {
	out := &partialStreamOutput{}
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"content":"Hel"}}]}`), out)
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`), out)
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"reasoning":"think"}}]}`), out)
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"reasoning":" more"}}]}`), out)
	if out.Content != "Hello" {
		t.Fatalf("content = %q, want Hello", out.Content)
	}
	if out.Reasoning != "think more" {
		t.Fatalf("reasoning = %q, want 'think more'", out.Reasoning)
	}
	if !out.GotText {
		t.Fatal("GotText must be set after a content delta")
	}
}

func TestAccumulatePartialIgnoresEmptyAndUsages(t *testing.T) {
	out := &partialStreamOutput{}
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{}}]}`), out)
	accumulatePartial([]byte(`{"choices":[]}`), out)
	accumulatePartial([]byte(`{}`), out)
	accumulatePartial([]byte(`{"usage":{"prompt_tokens":5}}`), out)
	if out.GotText || out.Content != "" {
		t.Fatalf("empty chunks must not count as text: %+v", out)
	}
}

func TestAccumulatePartialTracksToolCalls(t *testing.T) {
	out := &partialStreamOutput{}
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":""}}]}}]}`), out)
	if !out.GotToolCalls {
		t.Fatal("GotToolCalls must be set after a tool-call delta")
	}
	if out.GotText || out.Content != "" {
		t.Fatalf("a tool-call delta must not count as text: %+v", out)
	}

	empty := &partialStreamOutput{}
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"tool_calls":[]}}]}`), empty)
	if empty.GotToolCalls {
		t.Fatal("an empty tool_calls array must not set GotToolCalls")
	}
}

func TestAccumulatePartialReasoningOnlyIsNotText(t *testing.T) {
	out := &partialStreamOutput{}
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"reasoning_content":"let me think"}}]}`), out)
	accumulatePartial([]byte(`{"choices":[{"index":0,"delta":{"reasoning":"and more"}}]}`), out)
	if out.Reasoning != "let me thinkand more" {
		t.Fatalf("reasoning = %q", out.Reasoning)
	}
	if out.GotText || out.GotToolCalls || out.Content != "" {
		t.Fatalf("reasoning-only stream must not count as a non-empty completion: %+v", out)
	}
}

func TestAppendPartialChatHistoryAppendsAssistantMessage(t *testing.T) {
	original := []byte(`{"model":"native","messages":[{"role":"user","content":"hi"}]}`)
	partial := &partialStreamOutput{Content: "Hello there", Reasoning: "thinking", GotText: true}
	rewritten, err := appendPartialChatHistory(original, partial)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(rewritten, &body); err != nil {
		t.Fatalf("rewritten body invalid: %v (%s)", err, rewritten)
	}
	if len(body.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %s", len(body.Messages), rewritten)
	}
	last := body.Messages[1]
	if last["role"] != "assistant" || last["content"] != "Hello there" || last["reasoning_content"] != "thinking" {
		t.Fatalf("appended assistant message = %#v", last)
	}
	if body.Messages[0]["role"] != "user" || body.Messages[0]["content"] != "hi" {
		t.Fatalf("original message not preserved: %#v", body.Messages[0])
	}
	if !strings.Contains(string(rewritten), `"model":"native"`) {
		t.Fatalf("model field lost: %s", rewritten)
	}
}

func TestAppendPartialChatHistoryNoPartialIsNoop(t *testing.T) {
	original := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	rewritten, err := appendPartialChatHistory(original, &partialStreamOutput{})
	if err != nil {
		t.Fatal(err)
	}
	if string(rewritten) != string(original) {
		t.Fatalf("no-partial request must be unchanged: %s", rewritten)
	}
}

// continueConfig builds a two-provider streaming entry route that also
// declares the continue policy, for runner/server takeover tests.
func continueConfig(t *testing.T) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:2] // a, b
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		continueRule("standard", 90*time.Second, "full"),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

// TestContinueStreamExcludesUsedProviderAndResharesPartial pins the takeover
// contract: the continuation re-dispatches only to providers not already used
// (the broken one is excluded via the exclude set) and passes the reshared
// partial output as an assistant message appended to the request history.
func TestContinueStreamExcludesUsedProviderAndResharesPartial(t *testing.T) {
	compiled := continueConfig(t)
	calls := make(map[string]int)
	var bodies []byte
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			calls[target.Provider]++
			bodies = request.Body
			ev := make(chan StreamEvent, 3)
			ev <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"continued"}}]}`), Meaningful: true}
			close(ev)
			return ev, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	request := ExecuteRequest{
		Kind: RequestChat,
		Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	partial := &partialStreamOutput{Content: "let me think", Reasoning: "reasoning", GotText: true}
	// The broken provider "a" was already used; it must be excluded.
	selected, callErr := runner.ContinueStream(context.Background(), "standard", request, partial, []string{"a"})
	if callErr != nil {
		t.Fatalf("ContinueStream failed: %v", callErr)
	}
	if selected.Provider != "b" {
		t.Fatalf("continuation provider = %q, want b", selected.Provider)
	}
	if calls["a"] != 0 {
		t.Fatalf("broken provider a was re-contacted %d time(s); must be excluded", calls["a"])
	}
	if calls["b"] != 1 {
		t.Fatalf("provider b calls = %d, want 1", calls["b"])
	}
	if !strings.Contains(string(bodies), `"role":"assistant"`) || !strings.Contains(string(bodies), "let me think") {
		t.Fatalf("continuation body must carry reshared assistant message: %s", bodies)
	}
	if !strings.Contains(string(bodies), `"role":"user"`) {
		t.Fatalf("continuation body must preserve the original conversation: %s", bodies)
	}
}

// TestContinueStreamTerminalFailureAfterAllProvidersUsed verifies the takeover
// reports deterministically when no unused provider remains to continue.
func TestContinueStreamTerminalFailureAfterAllProvidersUsed(t *testing.T) {
	compiled := continueConfig(t)
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ev := make(chan StreamEvent, 1)
			ev <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"x"}}]}`), Meaningful: true}
			close(ev)
			return ev, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	request := ExecuteRequest{Kind: RequestChat, Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
	partial := &partialStreamOutput{Content: "x", GotText: true}
	if _, callErr := runner.ContinueStream(context.Background(), "standard", request, partial, []string{"a", "b"}); callErr == nil {
		t.Fatal("ContinueStream must fail when every provider is already used")
	}
}

// TestServerContinueTakeoverOnFinishReasonlessClose is the end-to-end contract
// of the in-gateway takeover: the winner (a) relays a meaningful chunk and
// then closes without a finish_reason; the client must NOT see an error but
// the continuation (b) — a fresh provider that stayed eligible — answering on
// the same SSE stream, ending with [DONE].
func TestServerContinueTakeoverOnFinishReasonlessClose(t *testing.T) {
	compiled := continueConfig(t)
	var bCalls int
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 3)
			if target.Provider == "a" {
				// a wins the initial race with one meaningful chunk, then closes
				// without finish_reason: the truncated-GLM-stream scenario.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-a"}}]}`), Meaningful: true}
				close(ch)
				return ch, nil
			}
			// provider b
			bCalls++
			if bCalls == 1 {
				// First call is the initial race: stay silent so a wins; when a
				// wins, the branch is cancelled and this channel closes.
				go func() {
					<-ctx.Done()
					close(ch)
				}()
				return ch, nil
			}
			// Continuation: answer, then finish.
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-b"}}]}`), Meaningful: true}
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
			close(ch)
			return ch, nil
		},
	}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// The client must see the original partial AND the continuation, with no
	// error frame, ending in a real [DONE] (a completed turn).
	if !strings.Contains(body, "from-a") {
		t.Fatalf("takeover must relay the original partial: %s", body)
	}
	if !strings.Contains(body, "from-b") {
		t.Fatalf("takeover must relay the continuation: %s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("takeover must not surface a client-facing error: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("takeover must complete the turn with [DONE]: %s", body)
	}
	if bCalls != 2 {
		t.Fatalf("provider b must be contacted exactly twice (race + continuation), got %d", bCalls)
	}
	// The broken provider cools down; the takeover target does not.
	if got := runner.availableTargets([]Target{{Provider: "a", Model: "native-model"}}); len(got) != 0 {
		t.Fatalf("provider a must cool down after its break, available: %#v", got)
	}
	if got := runner.availableTargets([]Target{{Provider: "b", Model: "native-model"}}); len(got) != 1 {
		t.Fatalf("provider b must stay healthy after the takeover, available: %#v", got)
	}
}

// TestServerContinueTakeoverOnEmptyCompletion pins the empty-completion
// contract: a winner chat stream that ends WITH a finish_reason but relayed
// neither text content nor a tool-call (observed 2026-09-18: GLM-5.3-Flash
// emitting 3010 reasoning tokens and 0 content, then finishing; and carriers
// capping completions at 4096 tokens) is not an answer. The client must not
// see a [DONE] success on an empty turn; with the continue rule the gateway
// takes over to the next provider on the same SSE stream, and without the
// rule the empty completion must surface as a retryable error.
func TestServerContinueTakeoverOnEmptyCompletion(t *testing.T) {
	compiled := continueConfig(t)
	var bCalls int
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 3)
			if target.Provider == "a" {
				// a wins the initial race on its reasoning delta, then ends with a
				// normal finish_reason and zero content: the observed GLM case.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"reasoning_content":"leaking thought"}}]}`), Meaningful: true}
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
				close(ch)
				return ch, nil
			}
			// provider b
			bCalls++
			if bCalls == 1 {
				// Initial race: stay silent so a wins; closed when a wins.
				go func() {
					<-ctx.Done()
					close(ch)
				}()
				return ch, nil
			}
			// Continuation: answer for real, then finish.
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"real-answer"}}]}`), Meaningful: true}
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
			close(ch)
			return ch, nil
		},
	}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// The empty winner must not end the turn: the continuation answers on the
	// same stream and [DONE] only follows the real answer.
	if strings.Contains(body, "data: [DONE]\n") && !strings.Contains(body, "real-answer") {
		t.Fatalf("empty winner must not end the turn with [DONE] without an answer: %s", body)
	}
	if !strings.Contains(body, "real-answer") {
		t.Fatalf("takeover after empty completion must relay the real answer: %s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("takeover after empty completion must not surface a client-facing error: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("takeover must complete the turn with a final [DONE]: %s", body)
	}
	if bCalls != 2 {
		t.Fatalf("provider b must be contacted exactly twice (race + continuation), got %d", bCalls)
	}
	if got := runner.availableTargets([]Target{{Provider: "a", Model: "native-model"}}); len(got) != 0 {
		t.Fatalf("provider a must cool down after an empty completion, available: %#v", got)
	}
	if got := runner.availableTargets([]Target{{Provider: "b", Model: "native-model"}}); len(got) != 1 {
		t.Fatalf("provider b must stay healthy after the takeover, available: %#v", got)
	}
}

// TestServerEmptyCompletionSurfacesErrorWithoutContinue pins the contract when
// no continue rule is present: an empty completion (finish_reason present, no
// content, no tool calls) must surface as a retryable upstream error with no
// [DONE], and the provider must cool down — not be relayed as a success.
func TestServerEmptyCompletionSurfacesErrorWithoutContinue(t *testing.T) {
	compiled := singleProviderStreamConfig(t)
	events := make(chan StreamEvent, 3)
	events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"reasoning_content":"thinking"}}]}`), Meaningful: true}
	events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
	close(events)
	executor := &fakeExecutor{stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
		return events, nil
	}}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	var payload struct {
		Error struct {
			Message    string `json:"message"`
			Type       string `json:"type"`
			StatusCode int    `json:"status_code"`
			Retryable  bool   `json:"retryable"`
			Partial    bool   `json:"partial"`
		} `json:"error"`
	}
	if err := json.Unmarshal(sseErrorData(t, body), &payload); err != nil {
		t.Fatalf("structured error payload: %v\n%s", err, body)
	}
	if payload.Error.Type != "5xx" || payload.Error.StatusCode != 502 {
		t.Errorf("empty completion must classify as 5xx: %+v", payload.Error)
	}
	if !payload.Error.Retryable || !payload.Error.Partial {
		t.Errorf("empty completion must be retryable+partial: %+v", payload.Error)
	}
	if strings.Contains(body, "data: [DONE]\n") {
		t.Fatalf("empty completion without continue must not end with [DONE]: %s", body)
	}
	if got := runner.availableTargets([]Target{{Provider: "a", Model: "native-model"}}); len(got) != 0 {
		t.Fatalf("provider a must cool down after an empty completion, available: %#v", got)
	}
}

// TestServerEmptyCompletionIsNotToolCallTurn guard-rails the tool-call
// exception: a stream that ends with a finish_reason and relays only a
// tool-call delta (no content text) is a legitimate turn and must keep
// relaying as a completed success, not be misclassified as an empty answer.
func TestServerEmptyCompletionIsNotToolCallTurn(t *testing.T) {
	compiled := singleProviderStreamConfig(t)
	events := make(chan StreamEvent, 3)
	events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]}}]}`), Meaningful: true}
	events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`), Meaningful: false}
	close(events)
	executor := &fakeExecutor{stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
		return events, nil
	}}
	api, _ := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	if strings.Contains(body, "event: error") {
		t.Fatalf("a tool-call completion must not be treated as empty: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("a tool-call completion must end with [DONE]: %s", body)
	}
	if !strings.Contains(body, `"tool_calls"`) || !strings.Contains(body, `"id":"call_1"`) {
		t.Fatalf("the tool-call delta must be relayed to the client: %s", body)
	}
}
