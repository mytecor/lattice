package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
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

// TestAppendPartialChatHistoryRefusesToReshareToolCallPartial pins the
// tool-call boundary guard: a partial that already carries a tool-call delta
// must not be reshaped into an assistant message. The relayed stream was cut
// mid/with tool-call — its arguments JSON is truncated or unfit to hand to a
// successor provider as prose — and re-dispatching with that prose in context
// historically made the successor echo the dangling tool-call as text plus a
// truncated structured tool-call ("tool-call written into the text with an
// empty call block below", seen on `standard`). The request must stay
// untouched so the caller surfaces a clean retryable error instead.
func TestAppendPartialChatHistoryRefusesToReshareToolCallPartial(t *testing.T) {
	original := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	// A partial with tool-call deltas AND some text content: the dangerous
	// shape, where text may be the dangling tool-call prose itself.
	partial := &partialStreamOutput{Content: "<invoke name=\"write\">", GotText: true, GotToolCalls: true}
	rewritten, err := appendPartialChatHistory(original, partial)
	if err != nil {
		t.Fatal(err)
	}
	if string(rewritten) != string(original) {
		t.Fatalf("a tool-call partial must not be reshared as assistant text: %s", rewritten)
	}

	// Even a completed tool-call (no finish_reason yet) must not be reshared
	// as prose: the successor provider would not reliably re-emit it as a
	// structured call.
	completed := &partialStreamOutput{GotToolCalls: true}
	rewritten, err = appendPartialChatHistory(original, completed)
	if err != nil {
		t.Fatal(err)
	}
	if string(rewritten) != string(original) {
		t.Fatalf("a completed tool-call partial must not be reshared either: %s", rewritten)
	}

	// Sanity: a plain text partial still reshapes as before.
	textOnly := &partialStreamOutput{Content: "thinking out loud", GotText: true}
	rewritten, err = appendPartialChatHistory(original, textOnly)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rewritten), "thinking out loud") {
		t.Fatalf("a text-only partial must still be reshared: %s", rewritten)
	}
}

// continueConfig builds a two-provider streaming entry route that also
// declares the continue policy, for runner/server takeover tests.
func continueConfig(t *testing.T) *compiledConfig {
	t.Helper()
	return continueConfigRetries(t, 0)
}

// continueConfigRetries builds a two-provider streaming entry route that
// declares the continue policy with the given whole-chain retry budget. The
// continue rule carries a short bounded wait (50ms) so exhaustion tests that
// hold the stream open and then surface the terminal error do not block on
// the production default horizon (10m); tests that need a different wait
// horizon use continueConfigWait explicitly.
func continueConfigRetries(t *testing.T, retries int) *compiledConfig {
	t.Helper()
	return continueConfigRetriesWait(t, retries, 50*time.Millisecond)
}

// continueConfigRetriesWait is continueConfigRetries with an explicit hold
// horizon for the continue rule.
func continueConfigRetriesWait(t *testing.T, retries int, wait time.Duration) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:2] // a, b
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		continueRuleWait("standard", 90*time.Second, "full", retries, wait),
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

// TestServerContinueTakeoverExcludesJustBrokenProvider pins the ordering
// contract of the takeover exclusion: the provider whose stream just broke
// must be excluded from the IMMEDIATE next takeover, not only from the one
// after. The break uses ErrorModelNotFound (not in allRetryableClasses) so the
// provider does NOT enter health cooldown — without the exclusion the broken
// provider would re-enter the continuation race and win again (it is still the
// fastest), making it impossible to "continue on other providers". The fix
// seeds the just-broke provider into the exclusion set before dispatch, the
// continuation lands on the other provider, and the turn completes.
func TestServerContinueTakeoverExcludesJustBrokenProvider(t *testing.T) {
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
	var aCalls, bCalls int
	winnerBreak := &CallError{Class: ErrorModelNotFound, Status: http.StatusNotFound}
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 3)
			if target.Provider == "a" {
				aCalls++
				// a wins every race it participates in (fast responder) but breaks
				// with a non-retryable stream error: no cooldown, so a stays fully
				// eligible and, without the exclusion, would keep winning the
				// continuation too.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-a"}}]}`), Meaningful: true}
				ch <- StreamEvent{Err: winnerBreak}
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
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-b"}}]}`), Meaningful: true}
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
			close(ch)
			return ch, nil
		},
	}
	api, _ := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// The takeover must flee the broken provider: the continuation answers on
	// `b`, never on `a`, and the turn completes with [DONE] and no error.
	if !strings.Contains(body, "from-b") {
		t.Fatalf("takeover must continue on the other provider (b): %s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("takeover must not surface a client-facing error: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("takeover must complete the turn with [DONE]: %s", body)
	}
	// a races the initial win only; it must never be re-contacted by the
	// takeover. b races the initial loss + the continuation.
	if aCalls != 1 {
		t.Fatalf("broken provider a must not be re-contacted by the takeover: calls=%d, want 1", aCalls)
	}
	if bCalls != 2 {
		t.Fatalf("provider b must be contacted for the race and the continuation: calls=%d, want 2", bCalls)
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

// TestServerContinueNeverCrossesToolCallBoundary pins the fix for the
// "tool-call written into the text with an empty call block below" symptom:
// when the relayed winner emits a tool-call delta and then stalls or closes
// without finish_reason, the in-gateway continuation MUST NOT re-dispatch to
// another provider — it could only reshare the dangling tool-call as prose and
// produce a truncated call on the client. The client gets a clean retryable
// partial error instead of a [DONE] success or a malformed continuation, and
// the other provider is never consumed.
func TestServerContinueNeverCrossesToolCallBoundary(t *testing.T) {
	compiled := continueConfig(t)
	var bCalls int
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 3)
			if target.Provider == "a" {
				// a wins the race on a tool-call delta (a large write call whose
				// arguments JSON was cut mid-stream), then closes without a
				// finish_reason: the exact mid-tool-call truncation observed on
				// `standard`.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write","arguments":"{\"path\":\"/tmp/x.go\",\""}}]}}]}`), Meaningful: true}
				close(ch)
				return ch, nil
			}
			// provider b
			bCalls++
			// Initial race: stay silent so a wins; closed when a wins. It must
			// never be contacted again for a continuation.
			go func() {
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		},
	}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// The tool-call delta itself is relayed (pi can see work started), but the
	// turn must NOT complete as a clean [DONE] success — that would be the
	// truncated tool-call symptom.
	if !strings.Contains(body, `"tool_calls"`) {
		t.Fatalf("the tool-call delta must be relayed: %s", body)
	}
	if strings.Contains(body, "data: [DONE]\n") {
		t.Fatalf("a truncated tool-call stream must not end with [DONE]: %s", body)
	}
	// A retryable partial error must surface so a retry-capable client re-issues
	// the request cleanly instead of completing on a dangling call.
	var payload struct {
		Error struct {
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
		t.Errorf("truncated tool-call must classify as 5xx: %+v", payload.Error)
	}
	if !payload.Error.Retryable || !payload.Error.Partial {
		t.Errorf("truncated tool-call must be retryable+partial: %+v", payload.Error)
	}
	// The continuation must never have consumed provider b.
	if bCalls != 1 {
		t.Fatalf("takeover must not cross the tool-call boundary; provider b calls = %d, want 1 (race only)", bCalls)
	}
	// Provider a cools down after the break.
	if got := runner.availableTargets([]Target{{Provider: "a", Model: "native-model"}}); len(got) != 0 {
		t.Fatalf("provider a must cool down after the truncated tool-call break, available: %#v", got)
	}
}

// TestServerChainRetryOnExhaustedChain pins the whole-chain retry contract:
// when every provider in the pool breaks during the request (the continue
// chain is exhausted), the gateway re-dispatches the whole chain from the top
// with the accumulated partial output reshared — so the client still gets a
// TestServerChainRetryFleesCoolingProvidersOnExhaustedChain pins the fix for
// the "upstream stream failed persists" report: when every provider in the
// pool broke mid-stream and is still cooling, the whole-chain retry must NOT
// re-race them (fail-open would re-hit the just-broke uplink it exists to
// flee, burning the retry budget on known-dead carriers and pushing the error
// to the client / pi which then restarts from scratch). The chain-retry
// dispatch honors cooldown strictly, so an all-cooling pool yields no winner:
// the re-dispatch does not contact any still-cooling provider and the request
// surfaces the terminal retryable partial error. The retry value is earned
// only once a provider's cooldown expires (see the clock test).
func TestServerChainRetryFleesCoolingProvidersOnExhaustedChain(t *testing.T) {
	compiled := continueConfigRetries(t, 1)
	var callsA, callsB int
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 3)
			switch target.Provider {
			case "a":
				callsA++
				// Initial race winner: relays partial, then breaks without a
				// finish_reason.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-a-part"}}]}`), Meaningful: true}
				close(ch)
				return ch, nil
			case "b":
				callsB++
				if callsB == 1 {
					// Initial race loser: silent until cancelled (a wins first).
					go func() {
						<-ctx.Done()
						close(ch)
					}()
					return ch, nil
				}
				// Takeover target: relays partial, then breaks too.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-b-part"}}]}`), Meaningful: true}
				close(ch)
				return ch, nil
			}
			panic("unknown provider")
		},
	}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// Both provider partials relayed on the same stream.
	if !strings.Contains(body, "from-a-part") || !strings.Contains(body, "from-b-part") {
		t.Fatalf("both provider partials must be relayed before the error: %s", body)
	}
	// The chain is exhausted: BOTH providers broke and are still cooling, so
	// the whole-chain retry must not re-race them. It surfaces the terminal
	// retryable partial error rather than burning the retry on known-dead
	// carriers or ending the turn with [DONE].
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
	if !payload.Error.Retryable || !payload.Error.Partial || payload.Error.StatusCode != 502 {
		t.Fatalf("exhausted all-cooling chain must surface retryable+partial 5xx: %+v", payload.Error)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("an exhausted all-cooling chain must not end with [DONE]: %s", body)
	}
	// The whole-chain retry starting pass honored cooldown: neither provider
	// is re-contacted after it broke, exactly as a continuation must flee a
	// still-cooling uplink. a races the win once; b races the initial loss
	// plus the single takeover.
	if callsA != 1 {
		t.Fatalf("provider a must not be re-contacted once it broke: calls=%d, want 1", callsA)
	}
	if callsB != 2 {
		t.Fatalf("provider b must be contacted for the race and the takeover only: calls=%d, want 2", callsB)
	}
	// The chain retry started (budget spent attempting a pass) but the pass
	// found no available provider (both cooling) and reported exhausted — not
	// completed, since no winner was selected.
	met := scrape(t, runner.metrics)
	if !strings.Contains(met, `llm_chain_retries_total{status="started"} 1`) {
		t.Errorf("the whole-chain retry must be recorded as started:\n%s", met)
	}
	if !strings.Contains(met, `llm_chain_retries_total{status="exhausted"} 1`) {
		t.Errorf("the all-cooling retry pass must be recorded as exhausted:\n%s", met)
	}
	if strings.Contains(met, `llm_chain_retries_total{status="completed"}`) {
		t.Errorf("no winner was selected, so completed must stay absent:\n%s", met)
	}
}

// scheduling.
func TestRunnerChainRetryReracesWholePoolWithResharedPartial(t *testing.T) {
	compiled := continueConfig(t)
	var callsA, callsB int
	var bodies [][]byte
	var mu sync.Mutex
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			mu.Lock()
			bodies = append(bodies, append([]byte(nil), request.Body...))
			mu.Unlock()
			ch := make(chan StreamEvent, 3)
			switch target.Provider {
			case "a":
				callsA++
				if callsA == 1 {
					ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-a-part"}}]}`), Meaningful: true}
					close(ch)
					return ch, nil
				}
				// Chain-rety pass: answers for real.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"final-answer"}}]}`), Meaningful: true}
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
				close(ch)
				return ch, nil
			case "b":
				callsB++
				if callsB == 1 {
					go func() {
						<-ctx.Done()
						close(ch)
					}()
					return ch, nil
				}
				if callsB == 2 {
					ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-b-part"}}]}`), Meaningful: true}
					close(ch)
					return ch, nil
				}
				go func() {
					<-ctx.Done()
					close(ch)
				}()
				return ch, nil
			}
			panic("unknown provider")
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	request := ExecuteRequest{Kind: RequestChat, Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
	ctx := context.Background()
	partial := &partialStreamOutput{}

	// Wave 1: initial selection — a wins (from-a-part) then breaks.
	sel, callErr := runner.SelectStream(ctx, "standard", request)
	if callErr != nil {
		t.Fatalf("select failed: %v", callErr)
	}
	if sel.Provider != "a" {
		t.Fatalf("initial winner = %q, want a", sel.Provider)
	}
	for _, ev := range sel.Buffered {
		accumulatePartial(ev.Data, partial)
	}
	partial.GotText = true
	sel.Cancel()

	// Wave 2: takeover to the one remaining provider — b wins then breaks.
	next, callErr := runner.ContinueStream(ctx, "standard", request, partial, []string{"a"})
	if callErr != nil {
		t.Fatalf("takeover failed: %v", callErr)
	}
	if next.Provider != "b" {
		t.Fatalf("takeover provider = %q, want b", next.Provider)
	}
	for _, ev := range next.Buffered {
		accumulatePartial(ev.Data, partial)
	}
	next.Cancel()

	// Wave 3: the chain is exhausted — every provider is broken.
	if _, callErr := runner.ContinueStream(ctx, "standard", request, partial, []string{"a", "b"}); callErr == nil {
		t.Fatal("ContinueStream must fail once every provider is broken")
	}

	// Wave 4: the whole-chain retry re-dispatches with an empty broken set,
	// resharing the accumulated partial — a races again and answers.
	retry, callErr := runner.ContinueStream(ctx, "standard", request, partial, nil)
	if callErr != nil {
		t.Fatalf("chain retry failed: %v", callErr)
	}
	if retry.Provider != "a" {
		t.Fatalf("chain retry provider = %q, want a", retry.Provider)
	}
	if callsA != 2 || callsB != 3 {
		t.Fatalf("chain retry must re-race the whole pool: callsA=%d callsB=%d, want 2/3", callsA, callsB)
	}
	lastBody := string(bodies[len(bodies)-1])
	if !strings.Contains(lastBody, `"role":"assistant"`) || !strings.Contains(lastBody, "from-a-part") || !strings.Contains(lastBody, "from-b-part") {
		t.Fatalf("chain retry must reshape the accumulated partial into assistant context: %s", lastBody)
	}
}

// TestServerChainRetryBudgetExhausted pins the bound: a finite chain-retry
// budget cannot loop forever. Every provider always breaks after relaying a
// partial, so no provider ever completes a turn. Cooldown is fail-open, so a
// chain retry still re-selects a winner (the race re-runs over the pool) —
// but that winner also breaks, and once the single retry is spent the next
// exhaustion has no budget left: the request surfaces the terminal retryable
// 502 rather than looping or completing with [DONE]. The metric records the
// one whole-chain retry as started+completed (it did select a winner) and
// never as exhausted (no budget round was fully empty).
func TestServerChainRetryBudgetExhausted(t *testing.T) {
	compiled := continueConfigRetries(t, 1)
	// Every provider always breaks after relaying a partial, so no provider
	// ever completes and both stay cooling for the whole request. The single
	// whole-chain retry pass therefore finds no available (non-cooling)
	// provider and exhausts the budget without selecting a winner — the
	// request surfaces the terminal retryable partial 502.
	breakAll := func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
		ch := make(chan StreamEvent, 3)
		ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`), Meaningful: true}
		close(ch)
		return ch, nil
	}
	api, runner := streamTestServer(t, compiled, &fakeExecutor{stream: breakAll})

	body := postStream(t, api.URL)

	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("an exhausted chain with a spent budget must not end with [DONE]: %s", body)
	}
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
	if !payload.Error.Retryable || !payload.Error.Partial {
		t.Errorf("exhausted chain with spent budget must stay retryable+partial: %+v", payload.Error)
	}
	if payload.Error.StatusCode != 502 {
		t.Errorf("exhausted chain must surface as 5xx: %+v", payload.Error)
	}
	// A single whole-chain retry ran (budget spent attempting a pass) but the
	// strict-cooldown pass found no available provider — both are still
	// cooling — so it reported exhausted rather than completed, and never
	// re-contacted the broken providers.
	met := scrape(t, runner.metrics)
	if !strings.Contains(met, `llm_chain_retries_total{status="started"} 1`) {
		t.Errorf("exactly one whole-chain retry must be recorded as started:\n%s", met)
	}
	if !strings.Contains(met, `llm_chain_retries_total{status="exhausted"} 1`) {
		t.Errorf("the retry pass over an all-cooling pool must be recorded as exhausted:\n%s", met)
	}
	if strings.Contains(met, `llm_chain_retries_total{status="completed"}`) {
		t.Errorf("no winner was selected (all providers cooling), so completed must stay absent:\n%s", met)
	}
}

// TestServerChainRetryBudgetZeroKeepsOldBehavior pins that chain retry is
// opt-in: a continue rule without retries surfaces the terminal error once the
// whole chain is exhausted, exactly as before the feature.
func TestServerChainRetryBudgetZeroKeepsOldBehavior(t *testing.T) {
	compiled := continueConfig(t) // retries 0
	breakAll := func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
		ch := make(chan StreamEvent, 3)
		ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`), Meaningful: true}
		close(ch)
		return ch, nil
	}
	api, runner := streamTestServer(t, compiled, &fakeExecutor{stream: breakAll})

	body := postStream(t, api.URL)

	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("retries=0 must keep surfacing terminal errors, not [DONE]: %s", body)
	}
	sseErrorData(t, body) // must be a valid structured SSE error
	// retries=0 means no whole-chain retry was ever attempted, so the
	// chain_retry metric family must stay absent (not a zeroed series).
	if got := scrape(t, runner.metrics); strings.Contains(got, "llm_chain_retries_total") {
		t.Errorf("retries=0 must not emit any whole-chain retry series:\n%s", got)
	}
}

// TestRunnerContinuationFleesCoolingProviderUntilCooldownExpires pins the
// strict-cooldown continuation contract introduced for the "upstream stream
// failed persists" report: a continuation must never re-race a provider that
// is still cooling (it just broke), even when that provider is the only other
// candidate and a non-strict (fresh-request) pool would fail-open onto it.
// The retry "second chance" is preserved: once the provider's cooldown
// expires, the continuation legitimately re-races it again. This is the
// time-based recovery the whole-chain-retry budget is meant to deliver — but
// only after the provider has had a real recovery window, never in the same
// instant it broke.
func TestRunnerContinuationFleesCoolingProviderUntilCooldownExpires(t *testing.T) {
	compiled := continueConfig(t) // providers a, b; retries 0
	current := time.Unix(1_700_000_000, 0)
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 3)
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"` + target.Provider + `-answer"}}]}`), Meaningful: true}
			close(ch)
			return ch, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.now = func() time.Time { return current }
	request := ExecuteRequest{Kind: RequestChat, Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
	ctx := context.Background()
	partial := &partialStreamOutput{Content: "x", GotText: true}

	// Both providers break and cool down, staggered so a's window expires
	// before b's. A strict continuation over an all-cooling pool must fail
	// (no winner) rather than fail-open and re-race the just-broke carriers —
	// the core of the fix.
	cooldown := compiled.providers["a"].Cooldown.Duration
	runner.RecordStreamFailure(ctx, "standard", "a", "native-model",
		&CallError{Class: ErrorUpstream, Status: 502})
	current = current.Add(time.Second) // b breaks one second after a
	runner.now = func() time.Time { return current }
	runner.RecordStreamFailure(ctx, "standard", "b", "native-model",
		&CallError{Class: ErrorUpstream, Status: 502})
	if _, callErr := runner.ContinueStream(ctx, "standard", request, partial, nil); callErr == nil {
		t.Fatal("a continuation over an all-cooling pool must fail, not fail-open onto the breakers")
	}

	// Advance the clock past a's cooldown only (b's window is still open): a
	// recovers, b stays cooling. A strict continuation now re-races the
	// survivor — a is the sole eligible candidate and is reached, preserving
	// the time-based "second chance".
	current = current.Add(cooldown - 500*time.Millisecond)
	runner.now = func() time.Time { return current }
	if got := runner.availableTargets([]Target{{Provider: "a", Model: "native-model"}}); len(got) != 1 {
		t.Fatalf("a must be eligible again once its cooldown expires, available: %#v", got)
	}
	if got := runner.availableTargets([]Target{{Provider: "b", Model: "native-model"}}); len(got) != 0 {
		t.Fatalf("b must still be cooling, available: %#v", got)
	}
	recovered, callErr := runner.ContinueStream(ctx, "standard", request, partial, nil)
	if callErr != nil {
		t.Fatalf("continuation after cooldown expiry failed: %v", callErr)
	}
	if recovered.Provider != "a" {
		t.Fatalf("the recovered provider must be re-raceable once its cooldown expires, got %q", recovered.Provider)
	}
	recovered.Cancel()
}
