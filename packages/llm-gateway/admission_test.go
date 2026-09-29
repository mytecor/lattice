package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func admissionRule(route string, maxInFlight, maxPending int, waitTimeout time.Duration) Rule {
	r := &AdmissionRule{}
	r.setIdentity(route, ActionAdmission)
	r.MaxInFlight = maxInFlight
	r.MaxPending = &maxPending
	r.WaitTimeout = Duration{Duration: waitTimeout}
	return r
}

func mapRuleDirect(route string, providers []string, nativeModel string, tier int) Rule {
	r := &MapRule{}
	r.setIdentity(route, ActionMap)
	r.Providers = providers
	r.Native = nativeModel
	r.Tier = tier
	return r
}

func TestAdmissionControllerLimitsAndQueues(t *testing.T) {
	metrics := newMetrics()
	ac := newAdmissionController(metrics, time.Now)
	policy := AdmissionConfig{
		MaxInFlight: 2,
		MaxPending:  2,
		WaitTimeout: 100 * time.Millisecond,
	}

	ctx := context.Background()
	rel1, err := ac.Admit(ctx, "standard", policy, time.Time{})
	if err != nil {
		t.Fatalf("first admit failed: %v", err)
	}
	defer rel1()

	rel2, err := ac.Admit(ctx, "standard", policy, time.Time{})
	if err != nil {
		t.Fatalf("second admit failed: %v", err)
	}
	defer rel2()

	// Third request must queue, then succeed when slot is released
	done := make(chan struct{})
	go func() {
		rel3, err := ac.Admit(ctx, "standard", policy, time.Time{})
		if err != nil {
			t.Errorf("queued admit failed: %v", err)
			return
		}
		rel3()
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	rel1()

	select {
	case <-done:
		// Succeeded
	case <-time.After(time.Second):
		t.Fatal("queued request was not woken up after release")
	}
}

func TestAdmissionControllerRejectsOnQueueFull(t *testing.T) {
	ac := newAdmissionController(newMetrics(), time.Now)
	policy := AdmissionConfig{
		MaxInFlight: 1,
		MaxPending:  1,
		WaitTimeout: 50 * time.Millisecond,
	}
	ctx := context.Background()
	rel1, err := ac.Admit(ctx, "standard", policy, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer rel1()

	// Waiter 1 in queue
	go func() {
		_, _ = ac.Admit(ctx, "standard", policy, time.Time{})
	}()
	time.Sleep(10 * time.Millisecond)

	// Waiter 2 exceeds maxPending=1 -> fast reject
	_, err = ac.Admit(ctx, "standard", policy, time.Time{})
	if err == nil || err.Status != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %#v", err)
	}
}

func TestAdmissionControllerClientDisconnectUnblocksQueue(t *testing.T) {
	ac := newAdmissionController(newMetrics(), time.Now)
	policy := AdmissionConfig{
		MaxInFlight: 1,
		MaxPending:  5,
		WaitTimeout: time.Second,
	}
	ctx := context.Background()
	rel1, err := ac.Admit(ctx, "standard", policy, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer rel1()

	cancelCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan *CallError, 1)
	go func() {
		_, err := ac.Admit(cancelCtx, "standard", policy, time.Time{})
		errCh <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil || err.Class != ErrorCancelled {
			t.Fatalf("expected cancelled error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled context did not unblock admit")
	}
}

func TestAdmissionContinuationReleaseHandoverNoLeak(t *testing.T) {
	cfg := testConfig()
	maxPending := 4
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a", "b"}, "model-x", 0),
		&AdmissionRule{
			ruleBase:    ruleBase{Route: "standard", Action: ActionAdmission},
			MaxInFlight: 1,
			MaxPending:  &maxPending,
			WaitTimeout: Duration{Duration: time.Second},
		},
		rankRule("standard"),
		raceRule("standard", 1),
		&RetryRule{ruleBase: ruleBase{Route: "standard", Action: ActionRetry}, Attempts: 2},
		&TimeoutRule{ruleBase: ruleBase{Route: "standard", Action: ActionTimeout}, Duration: Duration{Duration: 10 * time.Second}},
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, req ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 3)
			if target.Provider == "a" {
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-a"}}]}`), Meaningful: true}
				close(ch)
				return ch, nil
			}
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"from-b"}}]}`), Meaningful: true}
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
			close(ch)
			return ch, nil
		},
	}

	api, _ := streamTestServer(t, compiled, executor)
	defer api.Close()

	body1 := postStream(t, api.URL)
	if !strings.Contains(body1, "from-b") {
		t.Fatalf("takeover must relay the continuation from-b, got: %s", body1)
	}

	// Verify that the admission slot was cleanly released:
	// A second request must immediately succeed without queue rejection!
	body2 := postStream(t, api.URL)
	if !strings.Contains(body2, "from-b") {
		t.Fatalf("second request failed (possible admission leak), got: %s", body2)
	}
}

func TestMapRuleWithDirectProvidersAndTiers(t *testing.T) {
	rules := []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a", "b"}, "model-x", 0),
		mapRuleDirect("standard", []string{"c"}, "model-y", 1),
		rankRule("standard"),
		raceRule("standard", 1),
		&RetryRule{ruleBase: ruleBase{Route: "standard", Action: ActionRetry}},
		&TimeoutRule{ruleBase: ruleBase{Route: "standard", Action: ActionTimeout}, Duration: Duration{Duration: time.Minute}},
	}
	compiled, err := compileRules(rules...)
	if err != nil {
		t.Fatalf("compileRules failed: %v", err)
	}
	route := entryRoute(t, compiled, "standard")
	if len(route.Pool) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(route.Pool))
	}
	if route.Pool[0].Provider != "a" || route.Pool[0].Tier != 0 || route.Pool[0].Model != "model-x" {
		t.Errorf("unexpected target 0: %#v", route.Pool[0])
	}
	if route.Pool[1].Provider != "b" || route.Pool[1].Tier != 0 || route.Pool[1].Model != "model-x" {
		t.Errorf("unexpected target 1: %#v", route.Pool[1])
	}
	if route.Pool[2].Provider != "c" || route.Pool[2].Tier != 1 || route.Pool[2].Model != "model-y" {
		t.Errorf("unexpected target 2: %#v", route.Pool[2])
	}
	if !route.Retry.Internal {
		t.Fatalf("expected internal retry enabled")
	}
}

func TestRankRulePreservesTierOrder(t *testing.T) {
	cfg := testConfig()
	// Provider b has priority 100, provider a has priority 10
	cfg.Providers[1].Priority = 100 // b
	cfg.Providers[0].Priority = 10  // a
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a"}, "model-x", 0),
		mapRuleDirect("standard", []string{"b"}, "model-y", 1),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	route := compiled.models["standard"]
	if len(route.Pool) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(route.Pool))
	}
	// a (Tier 0) must precede b (Tier 1) even though b has higher priority!
	if route.Pool[0].Provider != "a" || route.Pool[0].Tier != 0 {
		t.Fatalf("tier 0 must precede tier 1 in rank, got target 0: %#v", route.Pool[0])
	}
	if route.Pool[1].Provider != "b" || route.Pool[1].Tier != 1 {
		t.Fatalf("tier 1 must be second in rank, got target 1: %#v", route.Pool[1])
	}
}

func TestExpectedTTFTBalancingShiftsLoadWhenFastProviderIsBusy(t *testing.T) {
	clock := newStoreAt(time.Now())
	clock.store.SetMaxConcurrent("fast", 4)
	clock.store.SetMaxConcurrent("slow", 4)

	// fast has EWMA TTFT 100ms, slow has EWMA TTFT 200ms
	clock.store.Observe("fast", nil, 100*time.Millisecond)
	clock.store.Observe("slow", nil, 200*time.Millisecond)

	policy := BalanceConfig{
		Enabled:     true,
		Strategy:    "expected-ttft",
		Window:      5 * time.Minute,
		ErrorBudget: 0.2,
	}
	targets := []Target{
		{Provider: "fast", Model: "m"},
		{Provider: "slow", Model: "m"},
	}

	// 1. Idle pool: fast wins
	selected := clock.store.Select("standard", targets, policy)
	if selected[0].Provider != "fast" {
		t.Fatalf("idle pool must choose fast provider, got %s", selected[0].Provider)
	}

	// 2. Put 4 in-flight on fast (reaches capacity limit):
	// fast gets over-capacity penalty -> score spikes -> slow wins!
	for range 4 {
		clock.store.IncrInFlight("fast")
	}
	selected = clock.store.Select("standard", targets, policy)
	if selected[0].Provider != "slow" {
		t.Fatalf("expected slow provider to win when fast is at capacity, got %s", selected[0].Provider)
	}
}

func TestExpectedTTFTSelectWithAllUnhealthyProviders(t *testing.T) {
	clock := newStoreAt(time.Now())
	clock.store.SetMaxConcurrent("a", 4)
	clock.store.SetMaxConcurrent("b", 4)

	// Both providers fail repeatedly -> health drops to 0
	for range 10 {
		clock.store.Observe("a", &CallError{Class: ErrorUpstream, Status: 500}, 100*time.Millisecond)
		clock.store.Observe("b", &CallError{Class: ErrorUpstream, Status: 500}, 200*time.Millisecond)
	}

	policy := BalanceConfig{
		Enabled:     true,
		Strategy:    "expected-ttft",
		Window:      5 * time.Minute,
		ErrorBudget: 0.2,
	}
	targets := []Target{
		{Provider: "b", Model: "m-b", Tier: 1},
		{Provider: "a", Model: "m-a", Tier: 0},
	}

	// Even when all providers are unhealthy, expected-ttft must still sort Tier 0 before Tier 1!
	selected := clock.store.Select("standard", targets, policy)
	if selected[0].Provider != "a" || selected[0].Tier != 0 {
		t.Fatalf("expected-ttft must preserve Tier 0 over Tier 1 even when all unhealthy, got %#v", selected[0])
	}
}

func TestInternalRetryHidesProviderErrorsFromClient(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a", "b"}, "model-x", 0),
		mapRuleDirect("standard", []string{"c"}, "model-y", 1),
		rankRule("standard"),
		balanceRule("standard", func(r *BalanceRule) { r.Strategy = "expected-ttft" }),
		raceRule("standard", 1),
		&RetryRule{ruleBase: ruleBase{Route: "standard", Action: ActionRetry}},
		&TimeoutRule{ruleBase: ruleBase{Route: "standard", Action: ActionTimeout}, Duration: Duration{Duration: 10 * time.Second}},
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var attempts atomic.Int32
	executor := &fakeExecutor{
		do: func(ctx context.Context, target Target, req ExecuteRequest) ([]byte, *CallError) {
			attempts.Add(1)
			if target.Provider == "a" {
				return nil, &CallError{Class: ErrorUpstream, Status: 503}
			}
			if target.Provider == "b" {
				return nil, &CallError{Class: ErrorRateLimit, Status: 429}
			}
			if target.Provider == "c" {
				return []byte(`{"id":"chat-1","choices":[{"message":{"content":"success-c"}}]}`), nil
			}
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	}

	runner := newRunner(compiled, newCatalog(compiled), executor)
	server := newServer(compiled, newCatalog(compiled), runner)

	reqBody := `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected client 200 OK, got status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "success-c") {
		t.Fatalf("expected response from provider c, got %s", rec.Body.String())
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 upstream attempts, got %d", attempts.Load())
	}
}

func TestInternalRetryFallbackToSameProviderDifferentTier(t *testing.T) {
	cfg := testConfig()
	// Provider a is in tier 0 with model-x (fails 404), and in tier 1 with model-y (succeeds)
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a"}, "model-x", 0),
		mapRuleDirect("standard", []string{"a"}, "model-y", 1),
		rankRule("standard"),
		raceRule("standard", 1),
		&RetryRule{ruleBase: ruleBase{Route: "standard", Action: ActionRetry}, Attempts: 2},
		&TimeoutRule{ruleBase: ruleBase{Route: "standard", Action: ActionTimeout}, Duration: Duration{Duration: 10 * time.Second}},
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var attempts atomic.Int32
	executor := &fakeExecutor{
		do: func(ctx context.Context, target Target, req ExecuteRequest) ([]byte, *CallError) {
			attempts.Add(1)
			if target.Model == "model-x" {
				return nil, &CallError{Class: ErrorNotFound, Status: 404}
			}
			if target.Model == "model-y" {
				return []byte(`{"id":"chat-1","choices":[{"message":{"content":"success-model-y"}}]}`), nil
			}
			return nil, &CallError{Class: ErrorInvalid, Status: 500}
		},
	}

	runner := newRunner(compiled, newCatalog(compiled), executor)
	server := newServer(compiled, newCatalog(compiled), runner)

	reqBody := `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected client 200 OK, got status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "success-model-y") {
		t.Fatalf("expected response from model-y, got %s", rec.Body.String())
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 attempts (model-x then model-y), got %d", attempts.Load())
	}
}

func TestStreamingInternalRetryHidesPreWinnerFailure(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a", "b"}, "model-x", 0),
		rankRule("standard"),
		raceRule("standard", 1),
		&RetryRule{ruleBase: ruleBase{Route: "standard", Action: ActionRetry}},
		&TimeoutRule{ruleBase: ruleBase{Route: "standard", Action: ActionTimeout}, Duration: Duration{Duration: 10 * time.Second}},
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, req ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 2)
			if target.Provider == "a" {
				stream <- StreamEvent{Err: &CallError{Class: ErrorUpstream, Status: 500}}
				close(stream)
				return stream, nil
			}
			go func() {
				stream <- StreamEvent{
					Data:       []byte(`data: {"choices":[{"delta":{"content":"token-b"}}]}`),
					Meaningful: true,
				}
				stream <- StreamEvent{
					Data: []byte(`data: [DONE]`),
					Done: true,
				}
				close(stream)
			}()
			return stream, nil
		},
	}

	runner := newRunner(compiled, newCatalog(compiled), executor)
	sel, callErr := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("expected SelectStream to succeed via retry, got: class=%q status=%d cause=%v", callErr.Class, callErr.Status, callErr.Cause)
	}
	if sel.Provider != "b" {
		t.Fatalf("expected winner b, got %s", sel.Provider)
	}
	if sel.Release != nil {
		sel.Release()
	}
}

func TestAdmissionRuleCompileValidation(t *testing.T) {
	// 1. Negative wait_timeout rejected
	_, err := compileRules(
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a"}, "model-x", 0),
		&AdmissionRule{
			ruleBase:    ruleBase{Route: "standard", Action: ActionAdmission},
			MaxInFlight: 4,
			WaitTimeout: Duration{Duration: -time.Second},
		},
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "wait_timeout must not be negative") {
		t.Fatalf("expected negative wait_timeout error, got %v", err)
	}

	// 2. Negative map tier rejected
	_, err = compileRules(
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a"}, "model-x", -1),
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "tier must not be negative") {
		t.Fatalf("expected negative tier error, got %v", err)
	}

	// 3. Admission on subroute rejected
	_, err = compileRules(
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a"}, "model-x", 0),
		rankRule("standard"),
		raceRule("standard", 1),
		retryRule("standard", "sub", 2),
		filterError("sub", "5xx"),
		filterProviderUnused("sub", "b"),
		mapRuleDirect("sub", []string{"b"}, "model-x", 0),
		&AdmissionRule{
			ruleBase:    ruleBase{Route: "sub", Action: ActionAdmission},
			MaxInFlight: 2,
		},
		rankRule("sub"),
		raceRule("sub", 1),
	)
	if err == nil || !strings.Contains(err.Error(), "subroute and must not set") {
		t.Fatalf("expected subroute admission rejection error, got %v", err)
	}

	// 4. Omitted maxPending defaults to 4 * maxInFlight
	compiled, err := compileRules(
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a"}, "model-x", 0),
		&AdmissionRule{
			ruleBase:    ruleBase{Route: "standard", Action: ActionAdmission},
			MaxInFlight: 3,
		},
		rankRule("standard"),
		raceRule("standard", 1),
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	route := entryRoute(t, compiled, "standard")
	if route.Admission.MaxPending != 12 {
		t.Fatalf("expected default maxPending=12, got %d", route.Admission.MaxPending)
	}
}

func TestAdmissionWaitConsumesLogicalDeadline(t *testing.T) {
	ac := newAdmissionController(newMetrics(), time.Now)
	policy := AdmissionConfig{MaxInFlight: 1, MaxPending: 1, WaitTimeout: time.Second}
	release, callErr := ac.Admit(context.Background(), "standard", policy, time.Time{})
	if callErr != nil {
		t.Fatal(callErr)
	}
	defer release()

	started := time.Now()
	_, callErr = ac.Admit(context.Background(), "standard", policy, started.Add(30*time.Millisecond))
	if callErr == nil || callErr.Class != ErrorTimeout {
		t.Fatalf("expected logical deadline timeout while queued, got %#v", callErr)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("admission ignored logical deadline and waited %s", elapsed)
	}
}

func TestProviderCapacityHeldForSelectedStreamLifetime(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.Providers[0].MaxConcurrent = 1
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a"}, "model-x", 0),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	executor := &fakeExecutor{stream: func(ctx context.Context, target Target, req ExecuteRequest) (<-chan StreamEvent, *CallError) {
		calls.Add(1)
		stream := make(chan StreamEvent, 1)
		stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"content":"token"}}]}`), Meaningful: true}
		go func() {
			<-ctx.Done()
			close(stream)
		}()
		return stream, nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)

	first, callErr := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatal(callErr)
	}
	secondResult := make(chan *SelectedStream, 1)
	secondError := make(chan *CallError, 1)
	go func() {
		selected, err := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		secondResult <- selected
		secondError <- err
	}()

	select {
	case <-secondResult:
		t.Fatal("second stream exceeded provider max_concurrent while first stream was active")
	case <-time.After(40 * time.Millisecond):
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one upstream stream before release, got %d", calls.Load())
	}

	first.Cancel()
	select {
	case second := <-secondResult:
		if err := <-secondError; err != nil {
			t.Fatal(err)
		}
		if second == nil {
			t.Fatal("second stream was not selected after capacity release")
		}
		second.Cancel()
	case <-time.After(time.Second):
		t.Fatal("provider capacity release did not wake the waiting scheduler")
	}
}

func TestInternalRetryExcludesEveryFailedRaceTarget(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a", "b", "c"}, "model-x", 0),
		rankRule("standard"),
		raceRule("standard", 2),
		&RetryRule{ruleBase: ruleBase{Route: "standard", Action: ActionRetry}, Attempts: 1},
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	calls := map[string]int{}
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, req ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
		mu.Unlock()
		if target.Provider == "c" {
			return []byte(`{"choices":[{"message":{"content":"ok"}}]}`), nil
		}
		return nil, &CallError{Class: ErrorUpstream, Status: 503, Scope: FailureScopeProvider}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatalf("retry did not reach unused target c: %v", callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"] != 1 || calls["b"] != 1 || calls["c"] != 1 {
		t.Fatalf("expected one call per target, got %#v", calls)
	}
}

func TestInternalHedgeReevaluatesCapacityAtFireTime(t *testing.T) {
	cfg := testConfig()
	for i := range cfg.Providers {
		cfg.Providers[i].MaxConcurrent = 1
	}
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		mapRuleDirect("standard", []string{"a", "b", "c"}, "model-x", 0),
		rankRule("standard"),
		raceRule("standard", 1),
		&HedgeRule{ruleBase: ruleBase{Route: "standard", Action: ActionHedge}, After: Duration{Duration: 40 * time.Millisecond}},
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	startedA := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, req ExecuteRequest) ([]byte, *CallError) {
		if target.Provider == "a" {
			close(startedA)
			<-ctx.Done()
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
		if target.Provider == "c" {
			return []byte(`{"choices":[{"message":{"content":"hedged"}}]}`), nil
		}
		return nil, &CallError{Class: ErrorUpstream, Status: 503, Scope: FailureScopeProvider}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	done := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		done <- callErr
	}()
	<-startedA
	if !runner.scores.TryAcquireProvider("b") {
		t.Fatal("failed to occupy provider b before hedge fired")
	}
	defer runner.scores.DecrInFlight("b")

	select {
	case callErr := <-done:
		if callErr != nil {
			t.Fatalf("dynamic hedge failed: %v", callErr)
		}
	case <-time.After(time.Second):
		t.Fatal("hedge did not re-evaluate capacity and dispatch provider c")
	}
}

func TestScopedInvalidUpstreamFailureIsReschedulable(t *testing.T) {
	if !isReschedulableError(&CallError{Class: ErrorInvalid, Status: 400, Scope: FailureScopeTarget}) {
		t.Fatal("target-scoped unsupported parameters must be reschedulable")
	}
	if isReschedulableError(&CallError{Class: ErrorInvalid, Status: 400, Scope: FailureScopeRequest}) {
		t.Fatal("request-scoped malformed input must remain terminal")
	}
}
