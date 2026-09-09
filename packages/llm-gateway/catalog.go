package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Catalog holds per-provider model discovery state. Catalogs, snapshots and
// refresh state are indexed by provider ID: every provider owns its own
// last-known-good snapshot and its explicit or inferred source, so exact
// native-model validation never depends on an unrelated provider.
type Catalog struct {
	sources map[string][]catalogSource
	client  *http.Client
	mu      sync.RWMutex
	models  map[string][]string
	updated map[string]time.Time
}

func newCatalog(config *compiledConfig) *Catalog {
	return &Catalog{
		sources: config.catalogSources,
		client:  &http.Client{Timeout: 30 * time.Second},
		models:  make(map[string][]string),
		updated: make(map[string]time.Time),
	}
}

// Validate checks the explicit native model of a target against the provider's
// last-known-good catalog snapshot before dispatch. It returns nil when the
// target is valid, a fail-closed error when an explicit catalog has not
// produced a snapshot yet, and model_not_found when a snapshot exists but does
// not contain the native ID. A provider without discovery or with an inferred
// catalog that has not produced a snapshot yet is optimistic: the explicitly
// configured native ID is called as configured. The lexicographic arbitrary
// fallback is removed entirely.
func (c *Catalog) Validate(providerID, native string) *CallError {
	sources := c.sources[providerID]
	if len(sources) == 0 {
		return nil // no discovery configured: the explicit native stays authoritative
	}
	c.mu.RLock()
	models, available := c.models[providerID]
	c.mu.RUnlock()
	if !available || len(models) == 0 {
		if catalogRequired(sources) {
			return &CallError{Class: ErrorInvalid, Status: 503, Cause: fmt.Errorf("provider %q has no last-known-good catalog (explicit models_url)", providerID)}
		}
		// Inferred catalog not yet available: optimistic call of the explicit ID.
		return nil
	}
	index := sort.SearchStrings(models, native)
	if index < len(models) && models[index] == native {
		return nil
	}
	return &CallError{Class: ErrorModelNotFound, Status: 404, Cause: fmt.Errorf("native model %q is not in the catalog of provider %q", native, providerID)}
}

// Refresh fetches every provider's catalog source concurrently and updates
// per-provider last-known-good snapshots. A failed refresh never destroys an
// existing snapshot for that provider. Returns errors keyed by provider ID.
func (c *Catalog) Refresh(ctx context.Context) map[string]error {
	errorsByProvider := make(map[string]error)
	for providerID, sources := range c.sources {
		type fetchResult struct {
			source catalogSource
			models []string
			err    error
		}
		results := make(chan fetchResult, len(sources))
		for _, source := range sources {
			source := source
			go func() {
				models, err := c.fetch(ctx, source)
				results <- fetchResult{source: source, models: models, err: err}
			}()
		}
		set := make(map[string]struct{})
		var failures []error
		for range sources {
			result := <-results
			if result.err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", result.source.URL, result.err))
				continue
			}
			for _, model := range result.models {
				set[model] = struct{}{}
			}
		}
		if len(set) == 0 {
			if len(failures) != 0 {
				errorsByProvider[providerID] = errors.Join(failures...)
			}
			continue
		}
		models := make([]string, 0, len(set))
		for model := range set {
			models = append(models, model)
		}
		sort.Strings(models)
		c.mu.Lock()
		c.models[providerID] = models
		c.updated[providerID] = time.Now()
		c.mu.Unlock()
	}
	return errorsByProvider
}

func catalogRequired(sources []catalogSource) bool {
	for _, source := range sources {
		if source.Explicit {
			return true
		}
	}
	return false
}

func (c *Catalog) fetch(ctx context.Context, source catalogSource) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	if source.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+source.APIKey)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("catalog returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	set := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" {
			set[id] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil, errors.New("catalog contains no models")
	}
	models := make([]string, 0, len(set))
	for id := range set {
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func (c *Catalog) Start(ctx context.Context, interval time.Duration, report func(map[string]error)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report(c.Refresh(ctx))
			}
		}
	}()
}

// Status reports per-provider catalog state for diagnostics. Provider IDs are
// internal identities and never reach client responses.
func (c *Catalog) Status() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	providers := make(map[string]any, len(c.sources))
	for providerID := range c.sources {
		providers[providerID] = map[string]any{
			"available":  len(c.models[providerID]) > 0,
			"models":     len(c.models[providerID]),
			"updated_at": c.updated[providerID],
		}
	}
	return providers
}
