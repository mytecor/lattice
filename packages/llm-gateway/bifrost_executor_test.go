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
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "mock-openai"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestBifrostKeyForVertexCarriesProjectRegionAndCredentials(t *testing.T) {
	provider := Provider{
		ID:                    "google-vertex",
		VertexProjectID:       "mytecor",
		VertexRegion:          "global",
		VertexAuthCredentials: `{"type":"service_account"}`,
	}
	key := bifrostKeyForProvider(provider, schemas.Vertex)
	if key.Value.GetValue() != "" {
		t.Fatal("OAuth-backed Vertex provider unexpectedly carries an API key")
	}
	if key.VertexKeyConfig == nil {
		t.Fatal("Vertex key config is missing")
	}
	if key.VertexKeyConfig.ProjectID.GetValue() != "mytecor" || key.VertexKeyConfig.Region.GetValue() != "global" {
		t.Fatalf("unexpected Vertex resource config: %#v", key.VertexKeyConfig)
	}
	if key.VertexKeyConfig.AuthCredentials.GetValue() != `{"type":"service_account"}` {
		t.Fatal("Vertex service-account credentials were not wired into the Bifrost key")
	}
}

func TestBifrostExecutorRegistersVertexAsStandardProvider(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = []Provider{{
		ID:                    "google-vertex",
		BaseProvider:          "vertex",
		InferenceURL:          "https://aiplatform.googleapis.com",
		VertexProjectID:       "mytecor",
		VertexRegion:          "global",
		VertexAuthCredentials: `{"type":"service_account"}`,
	}}
	cfg.RoutingRules = []Rule{
		filterModel("smart", "smart"),
		filterProvider("smart", "google-vertex"),
		mapRule("smart", "gemini-3.8-flash"),
		rankRule("smart"),
		raceRule("smart", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := newBifrostExecutor(context.Background(), compiled)
	if err != nil {
		t.Fatalf("Vertex must initialize through Bifrost's standard provider path: %v", err)
	}
	defer executor.Close()
	if got := executor.bifrostProviders["google-vertex"]; got != schemas.Vertex {
		t.Fatalf("google-vertex resolved to %q, want %q", got, schemas.Vertex)
	}
	request, callErr := executor.chatRequest(schemas.NewBifrostContext(context.Background(), schemas.NoDeadline), Target{
		Provider: "google-vertex",
		Model:    "gemini-3.8-flash",
	}, []byte(`{"messages":[{"role":"user","content":"hello"}]}`))
	if callErr != nil {
		t.Fatal(callErr)
	}
	if request.Provider != schemas.Vertex {
		t.Fatalf("request provider = %q, want %q", request.Provider, schemas.Vertex)
	}
	responsesRequest, callErr := executor.responsesRequest(schemas.NewBifrostContext(context.Background(), schemas.NoDeadline), Target{
		Provider: "google-vertex",
		Model:    "gemini-3.8-flash",
	}, []byte(`{"input":"hello"}`))
	if callErr != nil {
		t.Fatal(callErr)
	}
	if responsesRequest.Provider != schemas.Vertex {
		t.Fatalf("responses provider = %q, want %q", responsesRequest.Provider, schemas.Vertex)
	}
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

func TestStripParamsRemovesUnsupportedReasoningControl(t *testing.T) {
	// hyperfusion/litellm rejects zai's `thinking` control with 400; the
	// provider declares strip_params, and the gateway must drop the key
	// before sending while keeping the rest of the body intact.
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if _, leaked := body["thinking"]; leaked {
			t.Errorf("thinking reach upstream despite strip_params: %#v", body)
		}
		if _, leaked := body["reasoning_effort"]; leaked {
			t.Errorf("reasoning_effort reach upstream despite strip_params: %#v", body)
		}
		if body["model"] != "native-model" {
			t.Errorf("model rewrite lost: %#v", body["model"])
		}
		if body["messages"] == nil {
			t.Errorf("messages were dropped: %#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"id":"chat-1","object":"chat.completion","created":1,"model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	compiled := bifrostTestConfig(t, upstream.URL)
	compiled.raw.Providers[0].StripParams = []string{"thinking", "reasoning_effort"}
	compiled2, err := compileConfig(compiled.raw)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := newBifrostExecutor(context.Background(), compiled2)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	_, callErr := executor.Do(context.Background(), Target{Provider: "mock-openai", Model: "native-model"}, ExecuteRequest{
		Kind: RequestChat,
		Body: []byte(`{"model":"standard","thinking":{"type":"enabled"},"reasoning_effort":"high","messages":[{"role":"user","content":"hello"}]}`),
	})
	if callErr != nil {
		t.Fatal(callErr)
	}
}

func TestSetParamsForcesDisabledThinking(t *testing.T) {
	// The gateway hard-disables reasoning upstream where the provider accepts
	// the control: set_params forces thinking:{"type":"disabled"} and, being
	// applied after strip_params, wins even if the client sent its own value.
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		thinking, ok := body["thinking"].(map[string]any)
		if !ok {
			t.Fatalf("thinking control missing after set_params: %#v", body)
		}
		if thinking["type"] != "disabled" {
			t.Errorf("thinking control not forced to disabled: %#v", body["thinking"])
		}
		if body["model"] != "native-model" {
			t.Errorf("model rewrite lost: %#v", body["model"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"id":"chat-1","object":"chat.completion","created":1,"model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	compiled := bifrostTestConfig(t, upstream.URL)
	compiled.raw.Providers[0].StripParams = []string{"thinking"}
	compiled.raw.Providers[0].SetParams = map[string]json.RawMessage{
		"thinking": json.RawMessage(`{"type":"disabled"}`),
	}
	compiled2, err := compileConfig(compiled.raw)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := newBifrostExecutor(context.Background(), compiled2)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	_, callErr := executor.Do(context.Background(), Target{Provider: "mock-openai", Model: "native-model"}, ExecuteRequest{
		Kind: RequestChat,
		// The client tried to enable thinking; strip_params drops it, then
		// set_params forces the disabled value.
		Body: []byte(`{"model":"standard","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`),
	})
	if callErr != nil {
		t.Fatal(callErr)
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
		401: ErrorUpstream,
		402: ErrorUpstream,
		403: ErrorUpstream,
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
