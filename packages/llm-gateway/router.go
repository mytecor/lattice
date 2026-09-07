package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type ErrorClass string

const (
	ErrorTimeout    ErrorClass = "timeout"
	ErrorConnection ErrorClass = "connection_error"
	ErrorRateLimit  ErrorClass = "429"
	ErrorNotFound   ErrorClass = "404"
	ErrorUpstream   ErrorClass = "5xx"
	ErrorInvalid    ErrorClass = "invalid_response"
	ErrorCancelled  ErrorClass = "cancelled"
)

type CallError struct {
	Class  ErrorClass
	Status int
	Cause  error
}

func (e *CallError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Class)
}

type Target struct {
	Provider string
	Model    string
}

type Stage struct {
	Mode       string
	Providers  []string
	Retries    int
	RetryOn    map[ErrorClass]bool
	NextOn     map[ErrorClass]bool
	Backoff    BackoffConfig
	Timeout    time.Duration
	HedgeDelay time.Duration
}

type Plan struct {
	LogicalModel string
	Stages       []Stage
}

func compilePlans(rules []RoutingRule, providers map[string]Provider, mappings map[string]map[string]string) (map[string]Plan, error) {
	plans := make(map[string]Plan)
	for index, rule := range rules {
		model := strings.TrimSpace(rule.Match.Model)
		if model == "" {
			return nil, fmt.Errorf("routing rule %d has no match.model", index)
		}
		if _, ok := mappings[model]; !ok {
			return nil, fmt.Errorf("routing rule %d references unknown logical model %q", index, model)
		}
		plan := plans[model]
		plan.LogicalModel = model
		action := strings.ToLower(strings.TrimSpace(rule.Action))
		switch action {
		case "race", "hedge":
			stage, err := newStage(action, rule, providers, mappings[model])
			if err != nil {
				return nil, fmt.Errorf("routing rule %d: %w", index, err)
			}
			plan.Stages = append(plan.Stages, stage)
		case "retry":
			if len(plan.Stages) == 0 {
				return nil, fmt.Errorf("routing rule %d: retry requires a previous route", index)
			}
			if rule.Attempts < 1 {
				return nil, fmt.Errorf("routing rule %d: retry attempts must be positive", index)
			}
			stage := &plan.Stages[len(plan.Stages)-1]
			stage.Retries = rule.Attempts
			stage.RetryOn = parseErrorClasses(rule.On)
			if rule.Backoff != nil {
				stage.Backoff = *rule.Backoff
			}
		case "timeout":
			if len(plan.Stages) == 0 || rule.Duration.Duration <= 0 {
				return nil, fmt.Errorf("routing rule %d: timeout requires a previous route and positive duration", index)
			}
			plan.Stages[len(plan.Stages)-1].Timeout = rule.Duration.Duration
		case "fallback":
			if len(plan.Stages) == 0 {
				return nil, fmt.Errorf("routing rule %d: fallback requires a previous route", index)
			}
			plan.Stages[len(plan.Stages)-1].NextOn = parseErrorClasses(rule.On)
			mode := rule.FallbackStrategy
			if mode == "" {
				mode = "serial"
			}
			stage, err := newStage(mode, rule, providers, mappings[model])
			if err != nil {
				return nil, fmt.Errorf("routing rule %d: %w", index, err)
			}
			plan.Stages = append(plan.Stages, stage)
		default:
			return nil, fmt.Errorf("routing rule %d has unsupported action %q", index, action)
		}
		plans[model] = plan
	}
	return plans, nil
}

func newStage(mode string, rule RoutingRule, providers map[string]Provider, mappings map[string]string) (Stage, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "race" && mode != "hedge" && mode != "serial" {
		return Stage{}, fmt.Errorf("unsupported dispatch mode %q", mode)
	}
	if len(rule.Providers) == 0 {
		return Stage{}, errors.New("route requires at least one provider")
	}
	seen := make(map[string]struct{}, len(rule.Providers))
	ordered := append([]string(nil), rule.Providers...)
	for _, id := range ordered {
		provider, ok := providers[id]
		if !ok {
			return Stage{}, fmt.Errorf("unknown provider %q", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return Stage{}, fmt.Errorf("duplicate provider %q", id)
		}
		seen[id] = struct{}{}
		if _, ok := mappings[provider.Name]; !ok {
			return Stage{}, fmt.Errorf("logical model has no mapping for provider %q access group %q", id, provider.Name)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return providers[ordered[i]].Priority > providers[ordered[j]].Priority
	})
	stage := Stage{
		Mode:       mode,
		Providers:  ordered,
		RetryOn:    allRetryableClasses(),
		NextOn:     allRetryableClasses(),
		HedgeDelay: rule.After.Duration,
	}
	if stage.Mode == "hedge" && stage.HedgeDelay <= 0 {
		return Stage{}, errors.New("hedge requires a positive after duration")
	}
	return stage, nil
}

func parseErrorClasses(values []string) map[ErrorClass]bool {
	if len(values) == 0 {
		return allRetryableClasses()
	}
	classes := make(map[ErrorClass]bool, len(values))
	for _, value := range values {
		classes[ErrorClass(strings.ToLower(strings.TrimSpace(value)))] = true
	}
	return classes
}

func allRetryableClasses() map[ErrorClass]bool {
	return map[ErrorClass]bool{
		ErrorTimeout: true, ErrorConnection: true, ErrorRateLimit: true,
		ErrorNotFound: true, ErrorUpstream: true, ErrorInvalid: true,
	}
}

type RequestKind string

const (
	RequestChat      RequestKind = "chat"
	RequestResponses RequestKind = "responses"
)

type ExecuteRequest struct {
	Kind RequestKind
	Body []byte
}

type StreamEvent struct {
	Data       []byte
	Event      string
	Meaningful bool
	Done       bool
	Err        *CallError
}

type Executor interface {
	Do(context.Context, Target, ExecuteRequest) ([]byte, *CallError)
	Stream(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError)
	Close() error
}

type Runner struct {
	config   *compiledConfig
	catalog  *Catalog
	executor Executor
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	mu       sync.Mutex
	cooling  map[string]time.Time
}

func newRunner(config *compiledConfig, catalog *Catalog, executor Executor) *Runner {
	return &Runner{
		config: config, catalog: catalog, executor: executor,
		now: time.Now, sleep: sleepContext, cooling: make(map[string]time.Time),
	}
}

func (r *Runner) Run(ctx context.Context, logical string, request ExecuteRequest) ([]byte, *CallError) {
	plan, ok := r.config.plans[logical]
	if !ok {
		return nil, &CallError{Class: ErrorInvalid, Status: 404}
	}
	var last *CallError
	for index, stage := range plan.Stages {
		body, callErr := r.runStage(ctx, logical, index+1, stage, request)
		if callErr == nil {
			return body, nil
		}
		last = callErr
		if index == len(plan.Stages)-1 || !stage.NextOn[callErr.Class] {
			break
		}
	}
	return nil, last
}

func (r *Runner) runStage(ctx context.Context, logical string, stageIndex int, stage Stage, request ExecuteRequest) ([]byte, *CallError) {
	attempts := stage.Retries + 1
	var last *CallError
	for attempt := 0; attempt < attempts; attempt++ {
		stageCtx := withRouteAttempt(ctx, stageIndex, attempt+1)
		cancel := func() {}
		if stage.Timeout > 0 {
			stageCtx, cancel = context.WithTimeout(ctx, stage.Timeout)
		}
		body, callErr := r.dispatch(stageCtx, logical, stage, request)
		cancel()
		if callErr == nil {
			return body, nil
		}
		last = callErr
		if attempt+1 == attempts || !stage.RetryOn[callErr.Class] {
			break
		}
		if err := r.sleep(ctx, backoffDuration(stage.Backoff, attempt)); err != nil {
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: err}
		}
	}
	return nil, last
}

func (r *Runner) dispatch(ctx context.Context, logical string, stage Stage, request ExecuteRequest) ([]byte, *CallError) {
	providers := r.availableProviders(stage.Providers)
	if len(providers) == 0 {
		providers = append([]string(nil), stage.Providers...)
	}
	switch stage.Mode {
	case "serial":
		var last *CallError
		for _, providerID := range providers {
			target, err := r.target(logical, providerID)
			if err != nil {
				last = err
				continue
			}
			body, callErr := r.executor.Do(ctx, target, request)
			callErr = normalizeContextError(ctx, callErr)
			r.record(providerID, callErr)
			if callErr == nil {
				return body, nil
			}
			last = callErr
		}
		return nil, last
	case "hedge":
		return r.parallel(ctx, logical, providers, stage.HedgeDelay, request)
	default:
		return r.parallel(ctx, logical, providers, 0, request)
	}
}

type callResult struct {
	provider string
	body     []byte
	err      *CallError
}

func (r *Runner) parallel(ctx context.Context, logical string, providers []string, delay time.Duration, request ExecuteRequest) ([]byte, *CallError) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan callResult, len(providers))
	for index, providerID := range providers {
		index, providerID := index, providerID
		go func() {
			if index > 0 && delay > 0 {
				if err := sleepContext(ctx, time.Duration(index)*delay); err != nil {
					results <- callResult{provider: providerID, err: &CallError{Class: ErrorCancelled, Status: 499, Cause: err}}
					return
				}
			}
			target, targetErr := r.target(logical, providerID)
			if targetErr != nil {
				results <- callResult{provider: providerID, err: targetErr}
				return
			}
			body, callErr := r.executor.Do(ctx, target, request)
			callErr = normalizeContextError(ctx, callErr)
			results <- callResult{provider: providerID, body: body, err: callErr}
		}()
	}
	var last *CallError
	for range providers {
		result := <-results
		r.record(result.provider, result.err)
		if result.err == nil {
			cancel()
			return result.body, nil
		}
		if result.err.Class != ErrorCancelled || last == nil {
			last = result.err
		}
	}
	if last == nil {
		last = &CallError{Class: ErrorInvalid, Status: 502}
	}
	return nil, last
}

func (r *Runner) target(logical, providerID string) (Target, *CallError) {
	provider, ok := r.config.providers[providerID]
	if !ok {
		return Target{}, &CallError{Class: ErrorInvalid, Status: 502}
	}
	primary := r.config.mappings[logical][provider.Name]
	model, err := r.catalog.Resolve(provider.Name, primary)
	if err != nil {
		return Target{}, &CallError{Class: ErrorInvalid, Status: 503, Cause: err}
	}
	return Target{Provider: providerID, Model: model}, nil
}

func (r *Runner) availableProviders(ids []string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	available := make([]string, 0, len(ids))
	for _, id := range ids {
		if until, ok := r.cooling[id]; !ok || !now.Before(until) {
			available = append(available, id)
		}
	}
	return available
}

func (r *Runner) record(providerID string, callErr *CallError) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if callErr == nil {
		delete(r.cooling, providerID)
		return
	}
	if allRetryableClasses()[callErr.Class] {
		r.cooling[providerID] = r.now().Add(r.config.providers[providerID].Cooldown.Duration)
	}
}

func backoffDuration(config BackoffConfig, retryIndex int) time.Duration {
	initial := config.Initial.Duration
	if initial <= 0 {
		initial = 100 * time.Millisecond
	}
	maximum := config.Max.Duration
	if maximum <= 0 {
		maximum = time.Second
	}
	if strings.ToLower(config.Type) != "exponential" {
		return min(initial, maximum)
	}
	delay := initial
	for range retryIndex {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	return min(delay, maximum)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizeContextError(ctx context.Context, callErr *CallError) *CallError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &CallError{Class: ErrorTimeout, Status: 504, Cause: ctx.Err()}
	}
	if errors.Is(ctx.Err(), context.Canceled) && callErr != nil {
		return &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
	}
	return callErr
}
