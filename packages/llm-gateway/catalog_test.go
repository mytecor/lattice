package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCatalogDeterministicFallbackAndLastKnownGood(t *testing.T) {
	var mu sync.RWMutex
	status := http.StatusOK
	body := `{"object":"list","data":[{"id":"zeta"},{"id":"alpha"},{"id":"alpha"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.RLock()
		defer mu.RUnlock()
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	compiled := raceOnlyConfig(t)
	compiled.groupSources = map[string][]catalogSource{
		"group": {{URL: server.URL, Explicit: true}},
	}
	catalog := newCatalog(compiled)
	if failures := catalog.Refresh(context.Background()); len(failures) != 0 {
		t.Fatalf("initial refresh failed: %v", failures)
	}
	if got, err := catalog.Resolve("group", "missing-primary"); err != nil || got != "alpha" {
		t.Fatalf("fallback must be sorted and deterministic, got %q, %v", got, err)
	}

	mu.Lock()
	status = http.StatusBadGateway
	body = `{"error":"unavailable"}`
	mu.Unlock()
	if failures := catalog.Refresh(context.Background()); len(failures) != 1 {
		t.Fatalf("failed refresh was not reported: %v", failures)
	}
	if got, err := catalog.Resolve("group", "missing-primary"); err != nil || got != "alpha" {
		t.Fatalf("last-known-good was lost, got %q, %v", got, err)
	}
}

func TestCatalogReturnsPrimaryWhenPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"data":[{"id":"native-model"},{"id":"alpha"}]}`))
	}))
	defer server.Close()
	compiled := raceOnlyConfig(t)
	compiled.groupSources = map[string][]catalogSource{
		"group": {{URL: server.URL, Explicit: true}},
	}
	catalog := newCatalog(compiled)
	catalog.Refresh(context.Background())
	if got, err := catalog.Resolve("group", "native-model"); err != nil || got != "native-model" {
		t.Fatalf("primary was not preferred: %q, %v", got, err)
	}
}

func TestCatalogFailsClosedWithoutSnapshot(t *testing.T) {
	compiled := raceOnlyConfig(t)
	compiled.groupSources = map[string][]catalogSource{
		"group": {{URL: "https://catalog.invalid/v1/models", Explicit: true}},
	}
	catalog := newCatalog(compiled)
	if model, err := catalog.Resolve("group", "native-model"); err == nil || model != "" {
		t.Fatalf("catalog without a snapshot did not fail closed: model=%q err=%v", model, err)
	}
}

func TestCatalogMergesImplicitSourcesAndToleratesPartialFailure(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Errorf("unexpected first catalog path %q", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"data":[{"id":"native-model"}]}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer second.Close()

	compiled := raceOnlyConfig(t)
	compiled.groupSources = map[string][]catalogSource{
		"group": {
			{URL: first.URL + "/v1/models"},
			{URL: second.URL + "/functions/v1/gonka/models"},
		},
	}
	catalog := newCatalog(compiled)
	failures := catalog.Refresh(context.Background())
	if len(failures) != 0 {
		t.Fatalf("successful source did not satisfy group refresh: %v", failures)
	}
	if got, err := catalog.Resolve("group", "native-model"); err != nil || got != "native-model" {
		t.Fatalf("successful catalog source was not retained: %q, %v", got, err)
	}
}

func TestImplicitCatalogFailureFallsBackToConfiguredPrimary(t *testing.T) {
	compiled := raceOnlyConfig(t)
	compiled.groupSources = map[string][]catalogSource{
		"group": {{URL: "https://catalog.invalid/v1/models"}},
	}
	catalog := newCatalog(compiled)
	if got, err := catalog.Resolve("group", "native-model"); err != nil || got != "native-model" {
		t.Fatalf("implicit catalog unexpectedly blocked inference: %q, %v", got, err)
	}
}

func TestCatalogFetchesDerivedProviderPaths(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		wantPath string
	}{
		{name: "versioned base", basePath: "/v1", wantPath: "/v1/models"},
		{name: "custom prefix", basePath: "/functions/v1/gonka", wantPath: "/functions/v1/gonka/models"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != test.wantPath {
					t.Errorf("catalog path = %q, want %q", request.URL.Path, test.wantPath)
				}
				if request.Header.Get("Authorization") != "Bearer provider-key" {
					t.Errorf("inferred catalog did not use provider credential")
				}
				_, _ = writer.Write([]byte(`{"data":[{"id":"native-model"}]}`))
			}))
			defer server.Close()

			cfg := testConfig()
			cfg.Providers = cfg.Providers[:1]
			cfg.Providers[0].InferenceURL = server.URL + test.basePath
			cfg.Providers[0].APIKey = "provider-key"
			cfg.Models = cfg.Models[:1]
			cfg.RoutingRules = cfg.RoutingRules[:3]
			compiled, err := compileConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			catalog := newCatalog(compiled)
			if failures := catalog.Refresh(context.Background()); len(failures) != 0 {
				t.Fatalf("derived catalog refresh failed: %v", failures)
			}
		})
	}
}
