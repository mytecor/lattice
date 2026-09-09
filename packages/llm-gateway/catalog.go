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

type Catalog struct {
	sources map[string][]catalogSource
	client  *http.Client
	mu      sync.RWMutex
	models  map[string][]string
	updated map[string]time.Time
}

func newCatalog(config *compiledConfig) *Catalog {
	return &Catalog{
		sources: config.groupSources,
		client:  &http.Client{Timeout: 30 * time.Second},
		models:  make(map[string][]string),
		updated: make(map[string]time.Time),
	}
}

func (c *Catalog) Resolve(group, primary string) (string, error) {
	sources := c.sources[group]
	if len(sources) == 0 {
		if primary == "" {
			return "", fmt.Errorf("access group %q has no primary model", group)
		}
		return primary, nil
	}
	c.mu.RLock()
	models, available := c.models[group]
	c.mu.RUnlock()
	if !available || len(models) == 0 {
		if !catalogRequired(sources) && primary != "" {
			return primary, nil
		}
		return "", fmt.Errorf("access group %q has no last-known-good catalog", group)
	}
	index := sort.SearchStrings(models, primary)
	if index < len(models) && models[index] == primary {
		return primary, nil
	}
	return models[0], nil
}

func (c *Catalog) Refresh(ctx context.Context) map[string]error {
	errorsByGroup := make(map[string]error)
	for group, sources := range c.sources {
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
				errorsByGroup[group] = errors.Join(failures...)
			}
			continue
		}
		models := make([]string, 0, len(set))
		for model := range set {
			models = append(models, model)
		}
		sort.Strings(models)
		c.mu.Lock()
		c.models[group] = models
		c.updated[group] = time.Now()
		c.mu.Unlock()
	}
	return errorsByGroup
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

func (c *Catalog) Status() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	groups := make(map[string]any, len(c.sources))
	for group := range c.sources {
		groups[group] = map[string]any{
			"available":  len(c.models[group]) > 0,
			"models":     len(c.models[group]),
			"updated_at": c.updated[group],
		}
	}
	return groups
}
