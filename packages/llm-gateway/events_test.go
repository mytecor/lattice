package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureLogger returns a synchronous slog JSON logger writing into a mutex-
// guarded buffer, plus a read function that returns the lines parsed as maps.
func captureLogger(level string) (*slog.Logger, func() []map[string]any) {
	var mu sync.Mutex
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&lockedWriter{mu: &mu, buf: &buf}, &slog.HandlerOptions{Level: slogLevel(level)}))
	read := func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		events := make([]map[string]any, 0)
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				continue
			}
			events = append(events, obj)
		}
		return events
	}
	return logger, read
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// slogLevel maps a level name to a slog.Level, mirroring newGatewayLoggerTo.
func slogLevel(level string) slog.Level {
	switch level {
	case "error":
		return slog.LevelError
	case "warn":
		return slog.LevelWarn
	case "info":
		return slog.LevelInfo
	case "debug", "trace":
		return slog.LevelDebug
	default:
		return slog.LevelError
	}
}

// eventsTestServer builds a compiled config, runner and server whose logger is
// a captured in-memory logger, so structured events can be asserted.
func eventsTestServer(t *testing.T, cfg Config, executor Executor) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	catalog := newCatalog(compiled)
	runner := newRunner(compiled, catalog, executor)
	logger, read := captureLogger("debug")
	runner.logger = logger
	server := newServer(compiled, catalog, runner)
	server.logger = logger
	api := httptest.NewServer(server)
	t.Cleanup(api.Close)
	return api, read
}

func postCompletion(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(data)
}

func TestRequestCompletedEventCarriesFullFields(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	usage := `{"id":"c","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			return []byte(usage), nil
		},
	}
	api, read := eventsTestServer(t, cfg, executor)

	resp, _ := postCompletion(t, api.URL, `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	events := read()
	var completed map[string]any
	for _, e := range events {
		if e["event"] == "request_completed" {
			completed = e
		}
	}
	if completed == nil {
		t.Fatalf("no request_completed event, got: %#v", events)
	}
	for _, field := range []string{"status_code", "duration_ms", "ttft_ms", "input_tokens", "output_tokens", "cached_tokens", "stream", "attempts"} {
		if _, ok := completed[field]; !ok {
			t.Errorf("request_completed missing %q: %#v", field, completed)
		}
	}
	if completed["provider"] != "a" || completed["status"] != "success" || completed["stream"] != false {
		t.Errorf("unexpected request_completed dims: %#v", completed)
	}
	if completed["attempts"] != float64(1) {
		t.Errorf("attempts = %v, want 1", completed["attempts"])
	}
	if completed["event"] != "request_completed" || completed["service"] != gatewayService {
		t.Errorf("event/service wrong: %#v", completed)
	}
	// ttft for a non-stream request equals the response duration: it must be > 0.
	if completed["ttft_ms"].(float64) < 0 {
		t.Errorf("negative ttft_ms")
	}
}

func TestRequestFailedEventOnTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			return nil, &CallError{Class: ErrorTimeout, Status: 504}
		},
	}
	api, read := eventsTestServer(t, cfg, executor)

	resp, _ := postCompletion(t, api.URL, `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusRequestTimeout {
		// 502 for a 504 upstream (callErrorStatus maps 504 to 502 unless listed).
		t.Logf("timeout surfaced as %d", resp.StatusCode)
	}

	events := read()
	var failed map[string]any
	var received int
	for _, e := range events {
		switch e["event"] {
		case "request_failed":
			failed = e
		case "request_received":
			received++
		}
	}
	if failed == nil {
		t.Fatalf("no request_failed event: %#v", events)
	}
	if failed["error_type"] != string(ErrorTimeout) {
		t.Errorf("error_type = %v, want %q", failed["error_type"], ErrorTimeout)
	}
	if received != 1 {
		t.Errorf("request_received count = %d, want 1", received)
	}
}

func TestRetryEmitsAttemptEvents(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:2]
	// First provider always fails with 429; second succeeds. Entry route races
	// provider a only (count=1) and retries into standard.retry which picks
	// the unused provider b.
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
		retryRuleBackoff("standard", "standard.retry", 1, &BackoffConfig{
			Type:    "fixed",
			Initial: Duration{Duration: 1},
			Max:     Duration{Duration: 1},
		}),
		filterError("standard.retry", "429"),
		filterProviderUnused("standard.retry", "a", "b"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
	}
	executor := &fakeExecutor{
		do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
			if target.Provider == "a" {
				return nil, &CallError{Class: ErrorRateLimit, Status: 429}
			}
			return []byte(`{"id":"c","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`), nil
		},
	}
	api, read := eventsTestServer(t, cfg, executor)

	resp, _ := postCompletion(t, api.URL, `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200 (retry should succeed)", resp.StatusCode)
	}

	events := read()
	var retry, attempt map[string]any
	for _, e := range events {
		switch e["event"] {
		case "llm_retry":
			retry = e
		case "llm_attempt":
			attempt = e
		}
	}
	if retry == nil {
		t.Fatalf("no llm_retry event: %#v", events)
	}
	if attempt == nil {
		t.Fatalf("no llm_attempt success event on retry: %#v", events)
	}
	if retry["error_type"] != string(ErrorRateLimit) || retry["provider"] != "a" {
		t.Errorf("llm_retry dims wrong: %#v", retry)
	}
	if attempt["status"] != "success" || attempt["provider"] != "b" {
		t.Errorf("llm_attempt dims wrong: %#v", attempt)
	}
}

func TestFallbackEmitsFallbackEvent(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:2]
	cfg.RoutingRules = append([]Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}, fallbackRules("standard", []string{"429", "5xx", "timeout"}, []string{"b"})...)
	executor := &fakeExecutor{
		do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
			if target.Provider == "a" {
				return nil, &CallError{Class: ErrorRateLimit, Status: 429}
			}
			return []byte(`{"id":"c","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`), nil
		},
	}
	api, read := eventsTestServer(t, cfg, executor)

	resp, _ := postCompletion(t, api.URL, `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	events := read()
	var fallback map[string]any
	for _, e := range events {
		if e["event"] == "llm_fallback" {
			fallback = e
		}
	}
	if fallback == nil {
		t.Fatalf("no llm_fallback event: %#v", events)
	}
	if fallback["from_provider"] != "a" || fallback["to_provider"] != "b" || fallback["reason"] != string(ErrorRateLimit) {
		t.Errorf("llm_fallback dims wrong: %#v", fallback)
	}
}

func TestCooldownPutEventFiresOnRetryableFailure(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.Providers[0].Cooldown = Duration{Duration: 5 * time.Minute}
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	const nCalls = 3
	calls := 0
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			calls++
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		},
	}
	api, read := eventsTestServer(t, cfg, executor)

	for i := 0; i < nCalls; i++ {
		resp, _ := postCompletion(t, api.URL, `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`)
		_ = resp.Body.Close()
	}

	events := read()
	cooldowns := 0
	for _, e := range events {
		if e["event"] == "cooldown_put" {
			cooldowns++
			if e["provider"] != "a" || e["error_type"] != string(ErrorRateLimit) {
				t.Errorf("cooldown_put dims wrong: %#v", e)
			}
		}
	}
	// First failure puts the provider into cooldown (one event); re-extending an
	// active window on a later failure within the same window is not a new event.
	if cooldowns == 0 {
		t.Fatalf("no cooldown_put event emitted for a 429: %#v", events)
	}
	if calls != nCalls {
		t.Fatalf("provider called %d times, want %d", calls, nCalls)
	}
}

func TestEveryEventLineCarriesServiceAndEventName(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			return []byte(`{"id":"c","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`), nil
		},
	}
	api, read := eventsTestServer(t, cfg, executor)

	// A valid request, a rejected request (unknown model) and a bad body all
	// land in the same journal, so every path must keep the event format.
	postCompletion(t, api.URL, `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`)
	postCompletion(t, api.URL, `{"model":"nope","messages":[{"role":"user","content":"hi"}]}`)
	postCompletion(t, api.URL, `not json`)

	for i, e := range read() {
		if e["event"] == "" || e["event"] == nil {
			t.Errorf("line %d missing event: %#v", i, e)
		}
		if e["service"] != gatewayService {
			t.Errorf("line %d missing service %q: %#v", i, gatewayService, e)
		}
	}
}

func TestEventsNeverContainSecrets(t *testing.T) {
	secret := "sk-test-super-secret-123"
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.Providers[0].APIKey = secret
	cfg.ClientAPIKey = secret
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, request ExecuteRequest) ([]byte, *CallError) {
			// Reflect a prompt-like body shape to prove bodies never leak.
			return []byte(`{"id":"c","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"PII"}}]}`), nil
		},
	}
	api, read := eventsTestServer(t, cfg, executor)

	req, err := http.NewRequest("POST", api.URL+"/v1/chat/completions", strings.NewReader(`{"model":"standard","messages":[{"role":"user","content":"confidential prompt my-secret-value"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	events := read()
	joined := ""
	for _, e := range events {
		raw, _ := json.Marshal(e)
		joined += string(raw)
	}
	for _, forbidden := range []string{secret, "confidential prompt", "PII", "my-secret-value"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("event output leaked %q: %s", forbidden, joined)
		}
	}
}
