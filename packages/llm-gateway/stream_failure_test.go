package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// singleProviderStreamConfig compiles the canonical single-provider stream
// entry route for logical model "standard": provider "a", race 1, the shared
// retry transition (429/5xx/timeout/connection_error) and a retry subroute
// over the same provider.
func singleProviderStreamConfig(t *testing.T) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
		retryRule("standard", "standard.retry", 1),
		filterError("standard.retry", "429", "5xx", "timeout", "connection_error"),
		filterProviderUnused("standard.retry", "a"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

// streamTestServer builds an httptest server plus its runner for the given
// config and executor, so tests can post streaming requests and inspect both
// the SSE body and the runner's health state.
func streamTestServer(t *testing.T, compiled *compiledConfig, executor Executor) (*httptest.Server, *Runner) {
	t.Helper()
	catalog := newCatalog(compiled)
	runner := newRunner(compiled, catalog, executor)
	api := httptest.NewServer(newServer(compiled, catalog, runner))
	t.Cleanup(api.Close)
	return api, runner
}

// postStream posts a streaming chat completion and returns the full response
// body (the caller drives how the fake executor terminates the stream, so the
// body always reaches a terminal state).
func postStream(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"standard","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream request status %d: %s", resp.StatusCode, body)
	}
	return string(body)
}

// meaningfulDelta is one chat-completion delta event that probeStream treats
// as the winner's first meaningful event.
func meaningfulDelta() StreamEvent {
	return StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`), Meaningful: true}
}

// sseErrorData extracts the data line of the single "event: error" frame.
func sseErrorData(t *testing.T, body string) []byte {
	t.Helper()
	const marker = "event: error\ndata: "
	index := strings.Index(body, marker)
	if index < 0 {
		t.Fatalf("no event: error frame in response: %s", body)
	}
	rest := body[index+len(marker):]
	end := strings.Index(rest, "\n\n")
	if end < 0 {
		t.Fatalf("unterminated event: error frame: %s", body)
	}
	return []byte(rest[:end])
}

// TestRecordStreamFailureCooldownHealthMetrics is the contract for mid-stream
// failure feedback: the provider enters cooldown, its health window records a
// health error, the branch attempt and stream break counters increment, and
// cancellations stay health-neutral.
func TestRecordStreamFailureCooldownHealthMetrics(t *testing.T) {
	compiled := raceOnlyConfig(t)
	runner := newRunner(compiled, newCatalog(compiled), &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) {
			return successBody("a"), nil
		},
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	})
	target := []Target{{Provider: "a", Model: "native-model"}}

	runner.RecordStreamFailure(context.Background(), "standard", "a", &CallError{Class: ErrorUpstream, Status: 502})
	if got := runner.availableTargets(target); len(got) != 0 {
		t.Fatalf("provider a must be cooling after a mid-stream 5xx, available: %#v", got)
	}
	if health := runner.scores.Health("a", 5*time.Minute, 0.2); health != 0 {
		t.Fatalf("provider a health after mid-stream 5xx = %v, want 0", health)
	}

	// Cancelled streams are health-neutral, exactly like cancelled branches.
	runner.RecordStreamFailure(context.Background(), "standard", "b", &CallError{Class: ErrorCancelled, Status: 499})
	if got := runner.availableTargets([]Target{{Provider: "b", Model: "native-model"}}); len(got) != 1 {
		t.Fatalf("cancelled mid-stream failure must not cool the provider")
	}
	if health := runner.scores.Health("b", 5*time.Minute, 0.2); health != 1 {
		t.Fatalf("provider b health after cancelled break = %v, want 1", health)
	}

	var scrape bytes.Buffer
	if err := runner.Metrics().WriteExposition(&scrape); err != nil {
		t.Fatal(err)
	}
	text := scrape.String()
	if !strings.Contains(text, `llm_stream_breaks_total{provider="a",error_type="5xx"} 1`) {
		t.Errorf("stream break counter missing/inaccurate:\n%s", text)
	}
	if !strings.Contains(text, `llm_attempts_total{provider="a",error_type="5xx"} 1`) {
		t.Errorf("branch attempt counter missing/inaccurate:\n%s", text)
	}
	if strings.Contains(text, `llm_stream_breaks_total{provider="b"`) {
		t.Errorf("cancelled break must not count as a stream break:\n%s", text)
	}

	// Nil is a no-op: RecordStreamFailure records failures only. A completed
	// stream's success is already recorded at selection time (TTFT branch),
	// and feeding it again here would double-count successes in the health
	// window.
	runner.RecordStreamFailure(context.Background(), "standard", "a", nil)
	if health := runner.scores.Health("a", 5*time.Minute, 0.2); health != 0 {
		t.Fatalf("nil must be a no-op, provider a health = %v, want 0", health)
	}
}

// TestStreamRetryableReflectsRouteErrorFilter pins the retryable semantics:
// an error admitted by the entry route's retry filter is retryable; one the
// filter excludes is not, even though it is in the shared retryable class set.
func TestStreamRetryableReflectsRouteErrorFilter(t *testing.T) {
	compiled, err := compileConfig(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	runner := newRunner(compiled, newCatalog(compiled), &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	})
	if !runner.streamRetryable("standard", &CallError{Class: ErrorUpstream, Status: 502}) {
		t.Fatal("5xx must be retryable when the entry retry filter admits it")
	}
	if runner.streamRetryable("standard", &CallError{Class: ErrorInvalid, Status: 502}) {
		t.Fatal("invalid_response must not be retryable: the entry retry filter excludes it")
	}
	if runner.streamRetryable("standard", nil) {
		t.Fatal("nil error is not retryable")
	}
}

// TestStreamMidStreamBreakEmitsStructuredError drives a winner stream that
// breaks after the first meaningful event: the client sees the buffered
// event, then a single structured event: error frame with typed fields, and
// no [DONE]; the provider enters cooldown.
func TestStreamMidStreamBreakEmitsStructuredError(t *testing.T) {
	compiled := singleProviderStreamConfig(t)
	events := make(chan StreamEvent, 3)
	events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`), Meaningful: false}
	events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`), Meaningful: true}
	events <- StreamEvent{Err: &CallError{Class: ErrorUpstream, Status: 502}}
	close(events)
	executor := &fakeExecutor{stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
		return events, nil
	}}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// The prelude and the meaningful delta are relayed first.
	if !strings.Contains(body, `"content":"hi"`) {
		t.Fatalf("meaningful delta missing from relayed stream: %s", body)
	}
	var payload struct {
		Error struct {
			Message    string `json:"message"`
			Type       string `json:"type"`
			StatusCode int    `json:"status_code"`
			Retryable  bool   `json:"retryable"`
			Partial    bool   `json:"partial"`
			RequestID  string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(sseErrorData(t, body), &payload); err != nil {
		t.Fatalf("structured error payload: %v\n%s", err, body)
	}
	if payload.Error.Message != "upstream stream failed" {
		t.Errorf("message must stay stable for text-matching clients: %q", payload.Error.Message)
	}
	if payload.Error.Type != "5xx" || payload.Error.StatusCode != 502 {
		t.Errorf("unexpected error classification: %+v", payload.Error)
	}
	if !payload.Error.Retryable || !payload.Error.Partial {
		t.Errorf("5xx after content must be retryable+partial: %+v", payload.Error)
	}
	if payload.Error.RequestID == "" {
		t.Errorf("request_id missing from error payload: %+v", payload.Error)
	}
	// No [DONE]: the stream did not complete.
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("[DONE] must not follow event: error: %s", body)
	}
	// The failure is fed back: provider a is cooling.
	if got := runner.availableTargets([]Target{{Provider: "a", Model: "native-model"}}); len(got) != 0 {
		t.Fatalf("provider a must be cooling after the mid-stream break, available: %#v", got)
	}
	var scrape bytes.Buffer
	if err := runner.Metrics().WriteExposition(&scrape); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scrape.String(), `llm_stream_breaks_total{provider="a",error_type="5xx"} 1`) {
		t.Errorf("stream break counter missing after mid-stream 5xx:\n%s", scrape.String())
	}
}

// TestStreamIdleWatchdogFiresOnSilentStream pins the stalled-stream watchdog:
// a winner that stops emitting events is cancelled after the idle timeout and
// the client receives a typed timeout error (no hang, no [DONE]).
func TestStreamIdleWatchdogFiresOnSilentStream(t *testing.T) {
	compiled := singleProviderStreamConfig(t)
	compiled.raw.StreamIdleTimeout = Duration{50 * time.Millisecond}
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`), Meaningful: true}
	executor := &fakeExecutor{stream: func(ctx context.Context, _ Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
		go func() {
			<-ctx.Done()
			close(events)
		}()
		return events, nil
	}}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

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
	if payload.Error.Type != "timeout" || payload.Error.StatusCode != 504 {
		t.Errorf("stall must surface as a 504 timeout: %+v", payload.Error)
	}
	if !payload.Error.Retryable {
		t.Errorf("stall must be retryable per the entry retry filter: %+v", payload.Error)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("[DONE] must not follow the stall error: %s", body)
	}
	if got := runner.availableTargets([]Target{{Provider: "a", Model: "native-model"}}); len(got) != 0 {
		t.Fatalf("stall must record the failure against the provider, available: %#v", got)
	}
}

// TestStreamIdleWatchdogSurvivesSlowStream pins the neutral side of the
// watchdog: events arriving inside the idle window keep the stream alive to
// its normal completion, and a completed stream sends [DONE] with no error.
func TestStreamIdleWatchdogSurvivesSlowStream(t *testing.T) {
	compiled := singleProviderStreamConfig(t)
	compiled.raw.StreamIdleTimeout = Duration{200 * time.Millisecond}
	executor := &fakeExecutor{stream: func(ctx context.Context, _ Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
		events := make(chan StreamEvent, 8)
		go func() {
			defer close(events)
			events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"a"}}]}`), Meaningful: true}
			for i := 0; i < 6; i++ {
				select {
				case <-ctx.Done():
					return
				case <-time.After(30 * time.Millisecond):
				}
				events <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"b"}}]}`), Meaningful: true}
			}
		}()
		return events, nil
	}}
	api, _ := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	if strings.Contains(body, "event: error") {
		t.Fatalf("live stream must not trip the watchdog: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("completed stream must end with [DONE]: %s", body)
	}
}
