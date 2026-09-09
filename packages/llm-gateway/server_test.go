package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerAuthModelsAndLogicalRewrite(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = cfg.RoutingRules[:1]
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
	cfg.RoutingRules = cfg.RoutingRules[:1]
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

func TestManualRefreshDoesNotExposeGroupNames(t *testing.T) {
	compiled := raceOnlyConfig(t)
	compiled.raw.ClientAPIKey = "client-secret"
	compiled.groupSources = map[string][]catalogSource{
		"internal-provider-group": {{URL: "://invalid"}},
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
	if response.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "internal-provider-group") {
		t.Fatalf("refresh response leaked topology: status=%d body=%s", response.StatusCode, body)
	}
}

func TestServerReportsFailureAfterStreamingWinner(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = cfg.RoutingRules[:1]
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
