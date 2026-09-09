package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServerAuthModelsAndLogicalRewrite(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		mapRule("standard", "native-model", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	compiled.raw.ClientAPIKey = "client-secret"
	var captured Target
	executor := &fakeExecutor{
		do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
			captured = target
			return []byte(`{"id":"chat-1","model":"native-model","extra_fields":{"provider":"secret-provider"},"choices":[]}`), nil
		},
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	}
	catalog := newCatalog(compiled)
	server := httptest.NewServer(newServer(compiled, catalog, newRunner(compiled, catalog, executor)))
	defer server.Close()

	unauthorized, err := http.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("models endpoint bypassed auth: %d", unauthorized.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/models", nil)
	request.Header.Set("Authorization", "Bearer client-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&models); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(models.Data) != 1 || models.Data[0].ID != "standard" {
		t.Fatalf("unexpected public model list: %#v", models.Data)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"standard","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer client-secret")
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.StatusCode, body)
	}
	if captured.Model != "native-model" {
		t.Fatalf("logical model was not resolved: %#v", captured)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "standard" {
		t.Fatalf("native model leaked: %s", body)
	}
	if _, leaked := payload["extra_fields"]; leaked {
		t.Fatalf("Bifrost routing metadata leaked: %s", body)
	}
}

func TestUnknownModelFailsBeforeExecutor(t *testing.T) {
	compiled := raceOnlyConfig(t)
	called := false
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) {
			called = true
			return nil, nil
		},
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			called = true
			return nil, nil
		},
	}
	catalog := newCatalog(compiled)
	server := httptest.NewServer(newServer(compiled, catalog, newRunner(compiled, catalog, executor)))
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"provider/model","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound || called {
		t.Fatalf("unknown model handling: status=%d called=%v", response.StatusCode, called)
	}
}

func TestServerStreamsWinnerWithLogicalModel(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		mapRule("standard", "native-model", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(ctx context.Context, target Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 2)
			stream <- StreamEvent{Data: []byte(`{"id":"chunk","model":"native-model","choices":[{"delta":{"role":"assistant"}}]}`)}
			stream <- StreamEvent{Data: []byte(`{"id":"chunk","model":"native-model","choices":[{"delta":{"content":"hello"}}]}`), Meaningful: true}
			close(stream)
			return stream, nil
		},
	}
	catalog := newCatalog(compiled)
	server := httptest.NewServer(newServer(compiled, catalog, newRunner(compiled, catalog, executor)))
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"standard","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stream status %d: %s", response.StatusCode, body)
	}
	text := string(body)
	if !strings.Contains(text, `"model":"standard"`) || strings.Contains(text, "native-model") {
		t.Fatalf("stream leaked native model: %s", text)
	}
	if !strings.Contains(text, `"content":"hello"`) || !strings.HasSuffix(text, "data: [DONE]\n\n") {
		t.Fatalf("stream framing mismatch: %s", text)
	}
}

func TestManualRefreshDoesNotExposeProviderNamespace(t *testing.T) {
	compiled := raceOnlyConfig(t)
	compiled.raw.ClientAPIKey = "client-secret"
	compiled.catalogSources = map[string][]catalogSource{
		"internal-provider-id": {{URL: "://invalid"}},
	}
	catalog := newCatalog(compiled)
	executor := &fakeExecutor{
		do:     func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) { return nil, nil },
	}
	server := httptest.NewServer(newServer(compiled, catalog, newRunner(compiled, catalog, executor)))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/admin/models/refresh", nil)
	request.Header.Set("Authorization", "Bearer client-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "internal-provider-id") {
		t.Fatalf("refresh response leaked topology: status=%d body=%s", response.StatusCode, body)
	}
}

func TestServerReportsFailureAfterStreamingWinner(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		mapRule("standard", "native-model", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 2)
			stream <- StreamEvent{Data: []byte(`{"model":"native-model","choices":[{"delta":{"content":"start"}}]}`), Meaningful: true}
			stream <- StreamEvent{Err: &CallError{Class: ErrorUpstream, Status: 503}}
			close(stream)
			return stream, nil
		},
	}
	catalog := newCatalog(compiled)
	server := httptest.NewServer(newServer(compiled, catalog, newRunner(compiled, catalog, executor)))
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"standard","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	text := string(body)
	if !strings.Contains(text, `"content":"start"`) || !strings.Contains(text, "event: error") || strings.Contains(text, "native-model") {
		t.Fatalf("started stream failure handling mismatch: %s", text)
	}
}

func affinityServerConfig(t *testing.T) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []Rule{
		mapRule("standard", "native-model", "a"),
		rankRule("standard"),
		affinityRule("standard", func(r *AffinityRule) {
			r.Sources = []string{"responses.conversation", "responses.previous_response_id"}
			r.TTL = Duration{time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestServerBindsResponseAffinityNonStreaming(t *testing.T) {
	compiled := affinityServerConfig(t)
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			return []byte(`{"id":"resp_1","object":"response","conversation":{"id":"conv_1"}}`), nil
		},
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	}
	catalog := newCatalog(compiled)
	runner := newRunner(compiled, catalog, executor)
	server := httptest.NewServer(newServer(compiled, catalog, runner))
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"standard","input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", response.StatusCode)
	}
	if provider, ok := runner.affinity.Lookup("resp_1"); !ok || provider != "a" {
		t.Fatalf("response id was not bound to the winner: %q %v", provider, ok)
	}
	if provider, ok := runner.affinity.Lookup("conv_1"); !ok || provider != "a" {
		t.Fatalf("conversation id was not bound to the winner: %q %v", provider, ok)
	}
}

func TestServerBindsResponseAffinityStreaming(t *testing.T) {
	compiled := affinityServerConfig(t)
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 3)
			stream <- StreamEvent{
				Event: "response.created",
				Data:  []byte(`{"type":"response.created","response":{"id":"resp_s1","conversation":{"id":"conv_s1"}}}`),
			}
			stream <- StreamEvent{
				Event:      "response.output_text.delta",
				Data:       []byte(`{"type":"response.output_text.delta","delta":"hi"}`),
				Meaningful: true,
			}
			close(stream)
			return stream, nil
		},
	}
	catalog := newCatalog(compiled)
	runner := newRunner(compiled, catalog, executor)
	server := httptest.NewServer(newServer(compiled, catalog, runner))
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"standard","stream":true,"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stream status %d: %s", response.StatusCode, body)
	}
	if provider, ok := runner.affinity.Lookup("resp_s1"); !ok || provider != "a" {
		t.Fatalf("streaming response id was not bound to the winner: %q %v", provider, ok)
	}
	if provider, ok := runner.affinity.Lookup("conv_s1"); !ok || provider != "a" {
		t.Fatalf("streaming conversation id was not bound to the winner: %q %v", provider, ok)
	}
}

func TestServerResponsesAffinityPinsSecondRequest(t *testing.T) {
	compiled := affinityServerConfig(t)
	// The behavior switch is atomic because cancelled branch goroutines may
	// still be draining after the first request.
	var failAll atomic.Bool
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			if failAll.Load() {
				return nil, &CallError{Class: ErrorUpstream, Status: 503}
			}
			return []byte(`{"id":"resp_1","object":"response"}`), nil
		},
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	}
	catalog := newCatalog(compiled)
	runner := newRunner(compiled, catalog, executor)
	server := httptest.NewServer(newServer(compiled, catalog, runner))
	defer server.Close()
	// First request binds resp_1 -> a.
	response, err := http.Post(server.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"standard","input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	response.Body.Close()
	// Second request with previous_response_id=resp_1 must be pinned to a even
	// though the executor would otherwise fail for the single pool member.
	failAll.Store(true)
	response, err = http.Post(server.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"standard","previous_response_id":"resp_1","input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	response.Body.Close()
	// The pinned provider failed: the route must fail closed (no other target),
	// propagating the pinned provider's error.
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("known affinity route must fail closed, got %d", response.StatusCode)
	}
}

func TestServerResponsesAffinityConversationPinsSecondRequest(t *testing.T) {
	compiled := affinityServerConfig(t)
	// The behavior switch is atomic because cancelled branch goroutines may
	// still be draining after the first request.
	var failAll atomic.Bool
	executor := &fakeExecutor{
		do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
			if failAll.Load() {
				return nil, &CallError{Class: ErrorUpstream, Status: 503}
			}
			return []byte(`{"id":"resp_1","object":"response","conversation":{"id":"conv_1"}}`), nil
		},
		stream: func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError) {
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	}
	catalog := newCatalog(compiled)
	runner := newRunner(compiled, catalog, executor)
	server := httptest.NewServer(newServer(compiled, catalog, runner))
	defer server.Close()
	// First request binds conv_1 -> a.
	response, err := http.Post(server.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"standard","input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	response.Body.Close()
	// Second request carrying the conversation identifier must be pinned to a
	// even though the executor would otherwise fail for the single pool member.
	failAll.Store(true)
	response, err = http.Post(server.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"standard","conversation":{"id":"conv_1"},"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	response.Body.Close()
	// The pinned provider failed: the route must fail closed (no other target),
	// propagating the pinned provider's error.
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("known conversation affinity must fail closed, got %d", response.StatusCode)
	}
}
