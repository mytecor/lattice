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
