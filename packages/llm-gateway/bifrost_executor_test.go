package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

func bifrostTestConfig(t *testing.T, upstreamURL string) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.Providers[0].ID = "mock-openai"
	cfg.Providers[0].InferenceURL = upstreamURL
	cfg.Providers[0].APIKey = "provider-secret"
	cfg.Providers[0].AllowPrivateNetwork = true
	cfg.RoutingRules = cfg.RoutingRules[:1]
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestBifrostExecutorCustomProviderChat(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Errorf("provider credential missing")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "native-model" {
			t.Errorf("logical model reached upstream: %#v", body["model"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"id":"chat-1","object":"chat.completion","created":1,"model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	compiled := bifrostTestConfig(t, upstream.URL)
	executor, err := newBifrostExecutor(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	body, callErr := executor.Do(context.Background(), Target{Provider: "mock-openai", Model: "native-model"}, ExecuteRequest{
		Kind: RequestChat,
		Body: []byte(`{"model":"standard","messages":[{"role":"user","content":"hello"}]}`),
	})
	if callErr != nil {
		t.Fatal(callErr)
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response["model"] != "native-model" {
		t.Fatalf("unexpected Bifrost response: %s", body)
	}
	if _, leaked := response["extra_fields"]; leaked {
		t.Fatalf("Bifrost internal metadata was not removed: %s", body)
	}
}

func TestBifrostExecutorCustomProviderChatWithVersionedBasePath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// A provider whose OpenAI-compatible server lives under an arbitrary
		// routed prefix (e.g. Supabase Edge Functions at
		// "/functions/v1/gonka") must not have a duplicate "/v1" re-appended.
		if request.URL.Path != "/functions/v1/gonka/chat/completions" {
			t.Errorf("unexpected path %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"id":"chat-1","object":"chat.completion","created":1,"model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	compiled := bifrostTestConfig(t, upstream.URL+"/functions/v1/gonka")
	executor, err := newBifrostExecutor(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	_, callErr := executor.Do(context.Background(), Target{Provider: "mock-openai", Model: "native-model"}, ExecuteRequest{
		Kind: RequestChat,
		Body: []byte(`{"model":"standard","messages":[{"role":"user","content":"hello"}]}`),
	})
	if callErr != nil {
		t.Fatal(callErr)
	}
}

func TestBifrostExecutorStreamingMeaningfulChunk(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher := writer.(http.Flusher)
		messages := []string{
			`{"id":"chat-1","object":"chat.completion.chunk","created":1,"model":"native-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`{"id":"chat-1","object":"chat.completion.chunk","created":1,"model":"native-model","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		}
		for _, message := range messages {
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", message)
			flusher.Flush()
		}
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	compiled := bifrostTestConfig(t, upstream.URL)
	executor, err := newBifrostExecutor(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	stream, callErr := executor.Stream(context.Background(), Target{Provider: "mock-openai", Model: "native-model"}, ExecuteRequest{
		Kind: RequestChat,
		Body: []byte(`{"model":"standard","stream":true,"messages":[{"role":"user","content":"hello"}]}`),
	})
	if callErr != nil {
		t.Fatal(callErr)
	}
	var meaningful []StreamEvent
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, open := <-stream:
			if !open {
				if len(meaningful) != 1 || !strings.Contains(string(meaningful[0].Data), "hello") {
					t.Fatalf("unexpected meaningful events: %#v", meaningful)
				}
				return
			}
			if event.Err != nil {
				t.Fatal(event.Err)
			}
			if event.Meaningful {
				meaningful = append(meaningful, event)
			}
		case <-deadline:
			t.Fatal("stream did not finish")
		}
	}
}

func TestBifrostExecutorResponses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			t.Errorf("unexpected path %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "native-model" {
			t.Errorf("logical Responses model reached upstream: %#v", body["model"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"id":"resp-1","object":"response","created_at":1,"completed_at":1,"status":"completed","model":"native-model","output":[],"error":null,"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()

	compiled := bifrostTestConfig(t, upstream.URL)
	executor, err := newBifrostExecutor(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	body, callErr := executor.Do(context.Background(), Target{Provider: "mock-openai", Model: "native-model"}, ExecuteRequest{
		Kind: RequestResponses,
		Body: []byte(`{"model":"standard","input":"hello"}`),
	})
	if callErr != nil {
		t.Fatal(callErr)
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response["model"] != "native-model" || response["object"] != "response" {
		t.Fatalf("unexpected Responses payload: %s", body)
	}
}

func TestBifrostExecutorResponsesStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher := writer.(http.Flusher)
		events := []struct {
			name string
			data string
		}{
			{
				name: "response.created",
				data: `{"type":"response.created","sequence_number":0,"response":{"id":"resp-1","object":"response","created_at":1,"status":"in_progress","model":"native-model","output":[],"error":null}}`,
			},
			{
				name: "response.output_text.delta",
				data: `{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg-1","output_index":0,"content_index":0,"delta":"hello"}`,
			},
			{
				name: "response.completed",
				data: `{"type":"response.completed","sequence_number":2,"response":{"id":"resp-1","object":"response","created_at":1,"completed_at":1,"status":"completed","model":"native-model","output":[],"error":null,"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			},
		}
		for _, event := range events {
			_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event.name, event.data)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	compiled := bifrostTestConfig(t, upstream.URL)
	executor, err := newBifrostExecutor(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	stream, callErr := executor.Stream(context.Background(), Target{Provider: "mock-openai", Model: "native-model"}, ExecuteRequest{
		Kind: RequestResponses,
		Body: []byte(`{"model":"standard","stream":true,"input":"hello"}`),
	})
	if callErr != nil {
		t.Fatal(callErr)
	}
	var events []StreamEvent
	for event := range stream {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		events = append(events, event)
	}
	if len(events) != 3 {
		t.Fatalf("unexpected Responses stream: %#v", events)
	}
	if events[0].Meaningful || !events[1].Meaningful || events[1].Event != "response.output_text.delta" {
		t.Fatalf("meaningful Responses event selection mismatch: %#v", events)
	}
}

func TestBifrostErrorClassification(t *testing.T) {
	for status, expected := range map[int]ErrorClass{
		429: ErrorRateLimit,
		404: ErrorNotFound,
		410: ErrorNotFound,
		503: ErrorUpstream,
		504: ErrorTimeout,
	} {
		status := status
		got := classifyBifrostError(context.Background(), &schemas.BifrostError{StatusCode: &status})
		if got.Class != expected {
			t.Fatalf("status %d classified as %s, want %s", status, got.Class, expected)
		}
	}
}
