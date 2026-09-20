package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withUsageBody returns a chat completion response carrying usage so the tokens
// metric is exercised end-to-end.
const withUsageBody = `{"id":"chat-1","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`

func TestMetricsFlowThroughServer(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		balanceRule("standard", func(r *BalanceRule) {
			r.Strategy = "adaptive"
		}),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			return []byte(withUsageBody), nil
		},
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	}
	catalog := newCatalog(compiled)
	runner := newRunner(compiled, catalog, executor)
	api := httptest.NewServer(newServer(compiled, catalog, runner))
	defer api.Close()

	// Fire a few chat completions with usage.
	body := `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < 3; i++ {
		resp, err := http.Post(api.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("chat completion status %d", resp.StatusCode)
		}
	}

	m := runner.Metrics()
	var scrape bytes.Buffer
	if err := m.WriteExposition(&scrape); err != nil {
		t.Fatal(err)
	}
	text := scrape.String()

	// Request-level counter: 3 successes on provider a/route standard.
	if !strings.Contains(text, `llm_requests_total{route="standard",model="standard",provider="a",native_model="native-model",status="success"} 3`) {
		t.Errorf("request counter missing/inaccurate:\n%s", text)
	}
	// Duration histogram count.
	if !strings.Contains(text, `llm_request_duration_seconds_count{route="standard",model="standard",provider="a",native_model="native-model"} 3`) {
		t.Errorf("request duration count missing:\n%s", text)
	}
	// Attempts: 3 successful attempts (empty error_type).
	if !strings.Contains(text, `llm_attempts_total{provider="a",native_model="native-model",error_type=""} 3`) {
		t.Errorf("attempt counter missing:\n%s", text)
	}
	// Tokens accumulated: 3 * (10 in, 20 out).
	if !strings.Contains(text, `llm_input_tokens_total{model="standard",provider="a",native_model="native-model"} 30`) {
		t.Errorf("input tokens missing:\n%s", text)
	}
	if !strings.Contains(text, `llm_output_tokens_total{model="standard",provider="a",native_model="native-model"} 60`) {
		t.Errorf("output tokens missing:\n%s", text)
	}
	// Balance health snapshot exists for provider a.
	if !strings.Contains(text, `llm_balance_health{provider="a"}`) {
		t.Errorf("balance health gauge missing:\n%s", text)
	}
	// Dedicated metrics endpoint also serves the same registry over the API
	// mux without authentication.
	metricsResp, err := http.Get(api.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer metricsResp.Body.Close()
	scrapeMetrics, _ := io.ReadAll(metricsResp.Body)
	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics over API mux returned %d", metricsResp.StatusCode)
	}
	if !strings.Contains(string(scrapeMetrics), "llm_requests_total") {
		t.Errorf("GET /metrics over API mux missing family")
	}
}
