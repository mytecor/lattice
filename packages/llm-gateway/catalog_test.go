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
	compiled.groupSource["group"] = catalogSource{URL: server.URL}
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
	compiled.groupSource["group"] = catalogSource{URL: server.URL}
	catalog := newCatalog(compiled)
	catalog.Refresh(context.Background())
	if got, err := catalog.Resolve("group", "native-model"); err != nil || got != "native-model" {
		t.Fatalf("primary was not preferred: %q, %v", got, err)
	}
}

func TestCatalogFailsClosedWithoutSnapshot(t *testing.T) {
	compiled := raceOnlyConfig(t)
	compiled.groupSource["group"] = catalogSource{URL: "https://catalog.invalid/v1/models"}
	catalog := newCatalog(compiled)
	if model, err := catalog.Resolve("group", "native-model"); err == nil || model != "" {
		t.Fatalf("catalog without a snapshot did not fail closed: model=%q err=%v", model, err)
	}
}
