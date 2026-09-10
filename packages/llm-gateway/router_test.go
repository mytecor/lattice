package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeExecutor struct {
	do     func(context.Context, Target, ExecuteRequest) ([]byte, *CallError)
	stream func(context.Context, Target, ExecuteRequest) (<-chan StreamEvent, *CallError)
}

func (f *fakeExecutor) Do(ctx context.Context, target Target, request ExecuteRequest) ([]byte, *CallError) {
	return f.do(ctx, target, request)
}

func (f *fakeExecutor) Stream(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
	return f.stream(ctx, target, request)
}

func (*fakeExecutor) Close() error { return nil }

type callResult struct {
	provider string
	body     []byte
	err      *CallError
}

// entryRules builds the entry route "standard" for logical model "standard"
// over the given providers with the given race count.
func entryRules(providers []string, raceCount int) []Rule {
	return []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", providers...),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", raceCount),
	}
}

// retryRules appends the bounded retry transition and the retry subroute.
func retryRules(route string, on []string, providers []string, attempts int) []Rule {
	return append(retryRulesUnused(route, on, providers, attempts),
		retryRule(route, route+".retry", attempts))
}

// retryRulesUnused returns the retry subroute rules (with the unused provider
// policy) without the transition rule itself.
func retryRulesUnused(route string, on []string, providers []string, _ int) []Rule {
	return []Rule{
		filterError(route+".retry", on...),
		filterProviderUnused(route+".retry", providers...),
		mapRule(route+".retry", "native-model"),
		rankRule(route + ".retry"),
		raceRule(route+".retry", 1),
	}
}

// fallbackRules returns the fallback transition plus the fallback subroute
// over the given providers, admitting the given error classes.
func fallbackRules(route string, on []string, providers []string) []Rule {
	return []Rule{
		fallbackRule(route, route+".fallback"),
		filterError(route+".fallback", on...),
		filterProvider(route+".fallback", providers...),
		mapRule(route+".fallback", "native-model"),
		rankRule(route + ".fallback"),
		raceRule(route+".fallback", 1),
	}
}

func raceOnlyConfig(t *testing.T) *compiledConfig {
	t.Helper()
	compiled, err := compileConfig(testConfigWithRules(testConfig(), entryRules([]string{"a", "b"}, 2)))
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func testConfigWithRules(cfg Config, rules []Rule) Config {
	cfg.RoutingRules = rules
	return cfg
}

// rulesConfig returns a compiled config whose pool contains n providers of the
// shared registry (a..), ranked by descending priority (a first), with the
// given routing rules.
func rulesConfig(t *testing.T, n int, rules []Rule) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	providers := make([]Provider, 0, n)
	for i := 0; i < n; i++ {
		letter := rune('a' + i)
		providers = append(providers, Provider{
			ID:           string(letter),
			BaseProvider: "openai",
			InferenceURL: fmt.Sprintf("https://%c.invalid", letter),
			Priority:     100 - i,
		})
	}
	cfg.Providers = providers
	cfg.RoutingRules = rules
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func successBody(winner string) []byte {
	return []byte(`{"winner":"` + winner + `"}`)
}

// ---------------------------------------------------------------------------
// race bounds and first-success semantics
// ---------------------------------------------------------------------------

func TestBoundedRaceStartsExactlyTopCount(t *testing.T) {
	compiled := rulesConfig(t, 3, entryRules([]string{"a", "b", "c"}, 2))
	started := make(chan string, 3)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		select {
		case <-release:
			if target.Provider == "a" {
				return nil, &CallError{Class: ErrorUpstream, Status: 503}
			}
			return successBody(target.Provider), nil
		case <-ctx.Done():
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	result := make(chan callResult, 1)
	go func() {
		body, err := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		result <- callResult{body: body, err: err}
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case provider := <-started:
			seen[provider] = true
		case <-time.After(time.Second):
			t.Fatal("top-count providers did not start")
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("race must start the top-2 targets, got %#v", seen)
	}
	select {
	case provider := <-started:
		t.Fatalf("race launched a target outside the top-count batch: %s", provider)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	got := <-result
	if got.err != nil {
		t.Fatalf("race failed: %v", got.err)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.body, &payload); err != nil || payload["winner"] != "b" {
		t.Fatalf("unexpected winner: %s, %v", got.body, err)
	}
}

func TestBoundedRaceFirstErrorDoesNotFinishRequest(t *testing.T) {
	compiled := raceOnlyConfig(t)
	started := make(chan string, 2)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		select {
		case <-release:
			if target.Provider == "a" {
				return nil, &CallError{Class: ErrorUpstream, Status: 503}
			}
			return successBody("b"), nil
		case <-ctx.Done():
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	result := make(chan callResult, 1)
	go func() {
		body, err := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		result <- callResult{body: body, err: err}
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("providers did not start concurrently")
		}
	}
	close(release)
	got := <-result
	if got.err != nil {
		t.Fatalf("first error incorrectly won race: %v", got.err)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.body, &payload); err != nil || payload["winner"] != "b" {
		t.Fatalf("unexpected winner: %s, %v", got.body, err)
	}
}

func TestBoundedRaceCancelsLosers(t *testing.T) {
	compiled := raceOnlyConfig(t)
	started := make(chan string, 2)
	releaseWinner := make(chan struct{})
	loserCancelled := make(chan struct{})
	var once sync.Once
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		if target.Provider == "a" {
			<-releaseWinner
			return successBody("a"), nil
		}
		<-ctx.Done()
		once.Do(func() { close(loserCancelled) })
		return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	result := make(chan *CallError, 1)
	go func() {
		_, err := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		result <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("providers did not start")
		}
	}
	close(releaseWinner)
	if err := <-result; err != nil {
		t.Fatalf("race failed: %v", err)
	}
	select {
	case <-loserCancelled:
	case <-time.After(time.Second):
		t.Fatal("loser was not cancelled")
	}
}

// ---------------------------------------------------------------------------
// entry route selection
// ---------------------------------------------------------------------------

func TestEntryRouteSelectionByModel(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(entryRules([]string{"a", "b"}, 1),
		filterModel("stupid", "stupid"),
		filterProvider("stupid", "c"),
		mapRule("stupid", "native-model"),
		rankRule("stupid"),
		raceRule("stupid", 1),
	)
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(compiled.logicalIDs, ","); got != "standard,stupid" {
		t.Fatalf("entry model registry mismatch: %s", got)
	}
	started := make(chan string, 4)
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	// model=standard resolves the standard entry route (pool a,b, race 1).
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatalf("standard route failed: %v", callErr)
	}
	if provider := <-started; provider != "a" && provider != "b" {
		t.Fatalf("standard route must use its own pool, got %q", provider)
	}
	// model=stupid resolves the stupid entry route (pool c).
	if _, callErr := runner.Run(context.Background(), "stupid", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatalf("stupid route failed: %v", callErr)
	}
	if provider := <-started; provider != "c" {
		t.Fatalf("stupid route must use its own pool, got %q", provider)
	}
}

func TestDuplicateEntryModelRejected(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = append(cfg.RoutingRules,
		filterModel("second", "standard"),
		filterProvider("second", "c"),
		mapRule("second", "native-model"),
		rankRule("second"),
		raceRule("second", 1),
	)
	if _, err := compileConfig(cfg); err == nil || !strings.Contains(err.Error(), "duplicate entry route") {
		t.Fatalf("expected duplicate entry route rejection, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// retry subroute
// ---------------------------------------------------------------------------

func TestRetrySubroute429FiresRetry(t *testing.T) {
	compiled := rulesConfig(t, 3, append(
		append(entryRules([]string{"a", "b", "c"}, 2),
			retryRule("standard", "standard.retry", 1)),
		retryRulesUnused("standard", []string{"429", "5xx"}, []string{"a", "b", "c"}, 1)...,
	))
	var mu sync.Mutex
	calls := map[string]int{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
		mu.Unlock()
		if target.Provider == "c" {
			return successBody("c"), nil
		}
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	body, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("retry subroute did not recover: %v", callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 || calls["a"] != 1 || calls["b"] != 1 || calls["c"] != 1 {
		t.Fatalf("expected primary a,b plus retry c, got %#v", calls)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "c" {
		t.Fatalf("retry target did not win: %s", body)
	}
}

func TestRetrySubroute400NotApplicableReturnsOriginal(t *testing.T) {
	compiled := rulesConfig(t, 3, append(
		append(entryRules([]string{"a", "b", "c"}, 2),
			retryRule("standard", "standard.retry", 2)),
		retryRulesUnused("standard", []string{"429", "5xx"}, []string{"a", "b", "c"}, 1)...,
	))
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return nil, &CallError{Class: ErrorInvalid, Status: 400}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorInvalid || callErr.Status != 400 {
		t.Fatalf("original 400 must be returned, got %#v", callErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("retry fired for a non-applicable failure: %d calls", calls.Load())
	}
}

func TestRetrySubrouteAttemptsAndBackoff(t *testing.T) {
	var mu sync.Mutex
	var backoffs []time.Duration
	compiled := rulesConfig(t, 3, append(
		append(entryRules([]string{"a", "b"}, 2),
			retryRuleBackoff("standard", "standard.retry", 2,
				&BackoffConfig{Type: "exponential", Initial: Duration{time.Millisecond}, Max: Duration{4 * time.Millisecond}})),
		retryRulesUnused("standard", []string{"429"}, []string{"a", "b", "c"}, 2)...,
	))
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(_ context.Context, duration time.Duration) error {
		mu.Lock()
		backoffs = append(backoffs, duration)
		mu.Unlock()
		return nil
	}
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorRateLimit {
		t.Fatalf("expected terminal 429 after attempts, got %#v", callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	// a,b fail 429; attempt 1 runs c (only unused target), attempt 2 finds the
	// unused pool empty and stops there; two backoff sleeps ran.
	if len(backoffs) != 2 {
		t.Fatalf("expected two retry backoff sleeps, got %d", len(backoffs))
	}
	if calls.Load() != 3 {
		t.Fatalf("expected a,b,c calls, got %d", calls.Load())
	}
}

func TestRetrySubrouteUnusedPoolExhaustionReturnsOriginal(t *testing.T) {
	// The retry pool exactly equals the primary pool (a,b), so after the
	// primary uses them the retry subroute has no unused targets left: the
	// original failure must come back, never a spurious 502.
	rules := append(entryRules([]string{"a", "b"}, 2),
		retryRule("standard", "standard.retry", 2))
	rules = append(rules, filterError("standard.retry", "429"))
	rules = append(rules, filterProviderUnused("standard.retry", "a", "b"))
	rules = append(rules, mapRule("standard.retry", "native-model"), rankRule("standard.retry"), raceRule("standard.retry", 1))
	compiled := rulesConfig(t, 2, rules)
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorRateLimit {
		t.Fatalf("expected original 429 after retry pool exhaustion, got %#v", callErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("unused exhaustion must not repeat providers: %d calls", calls.Load())
	}
}

func TestRetrySubrouteUnusedPoolExhaustionContinuesToFallback(t *testing.T) {
	// The retry target is applicable to the 429 but cannot reuse a. That makes
	// only the retry transition unavailable; the sibling fallback must still
	// get the original 429 and recover through b.
	rules := append(entryRules([]string{"a"}, 1),
		retryRule("standard", "standard.retry", 1),
		fallbackRule("standard", "standard.fallback"))
	rules = append(rules,
		filterError("standard.retry", "429"),
		filterProviderUnused("standard.retry", "a"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
		filterError("standard.fallback", "429"),
		filterProvider("standard.fallback", "b"),
		mapRule("standard.fallback", "native-model"),
		rankRule("standard.fallback"),
		raceRule("standard.fallback", 1),
	)
	compiled := rulesConfig(t, 2, rules)
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		if target.Provider == "b" {
			return successBody("b"), nil
		}
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	body, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil || string(body) != string(successBody("b")) {
		t.Fatalf("fallback did not recover after empty retry target: body=%s err=%v", body, callErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one primary and one fallback call, got %d", calls.Load())
	}
}

func TestRouteTimeoutInterruptsRetryBackoff(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 1),
		retryRule("standard", "standard.retry", 1),
		timeoutRule("standard", 30*time.Millisecond))
	rules = append(rules, filterError("standard.retry", "429"))
	rules = append(rules, filterProviderUnused("standard.retry", "a", "b"))
	rules = append(rules, mapRule("standard.retry", "native-model"), rankRule("standard.retry"), raceRule("standard.retry", 1))
	compiled := rulesConfig(t, 2, rules)
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		if target.Provider == "b" {
			return successBody("b"), nil
		}
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	started := time.Now()
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorTimeout {
		t.Fatalf("route timeout during backoff was not preserved: %#v", callErr)
	}
	if elapsed := time.Since(started); elapsed >= 150*time.Millisecond {
		t.Fatalf("route timeout waited for full retry backoff: %s", elapsed)
	}
	if calls.Load() != 1 {
		t.Fatalf("new upstream call started after route timeout: %d", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// hedge
// ---------------------------------------------------------------------------

func TestHedgeStartsOnlyTargetAfterDelay(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 2),
		hedgeRule("standard", 40*time.Millisecond, "standard.hedge"))
	rules = append(rules, filterError("standard.hedge", "429", "5xx"))
	rules = append(rules, filterProviderUnused("standard.hedge", "b", "c"))
	rules = append(rules, mapRule("standard.hedge", "native-model"), rankRule("standard.hedge"), raceRule("standard.hedge", 1))
	compiled := rulesConfig(t, 3, rules)
	started := make(chan string, 4)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		select {
		case <-release:
			if target.Provider == "c" {
				return successBody("c"), nil
			}
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		case <-ctx.Done():
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	done := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		done <- callErr
	}()
	startTimes := make([]time.Time, 0, 3)
	for len(startTimes) < 2 {
		select {
		case <-started:
			startTimes = append(startTimes, time.Now())
		case <-time.After(time.Second):
			t.Fatalf("initial race did not start")
		}
	}
	// The hedge target (c, since b is already used) must start only after the
	// delay while the first two branches are still running.
	select {
	case <-started:
		startTimes = append(startTimes, time.Now())
	case <-time.After(time.Second):
		t.Fatalf("hedged target never started")
	}
	if elapsed := startTimes[2].Sub(startTimes[1]); elapsed < 25*time.Millisecond {
		t.Fatalf("hedge target started before the delay: %s", elapsed)
	}
	// The hedge must not clone the pool: with unused only one new target appears.
	select {
	case provider := <-started:
		t.Fatalf("hedge started more than one unused target: %s", provider)
	case <-time.After(80 * time.Millisecond):
	}
	close(release)
	if callErr := <-done; callErr != nil {
		t.Fatalf("hedged route failed: %v", callErr)
	}
}

func TestHedgeFastTerminalFailureSkipsHedgeAndTransitions(t *testing.T) {
	// A fast terminal failure continues into the retry transition without
	// waiting for the hedge window: the hedge is latency-only. The hedge delay
	// is made large so the test proves the retry runs well before it.
	rules := append(entryRules([]string{"a", "b"}, 2),
		retryRule("standard", "standard.retry", 1),
		hedgeRule("standard", 5*time.Second, "standard.hedge"))
	rules = append(rules, retryRulesUnused("standard", []string{"5xx"}, []string{"a", "b", "c"}, 1)...)
	rules = append(rules, filterError("standard.hedge", "5xx"))
	rules = append(rules, filterProviderUnused("standard.hedge", "b", "c"))
	rules = append(rules, mapRule("standard.hedge", "native-model"), rankRule("standard.hedge"), raceRule("standard.hedge", 1))
	compiled := rulesConfig(t, 3, rules)
	started := make(chan string, 4)
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		if target.Provider == "c" {
			return successBody("c"), nil
		}
		return nil, &CallError{Class: ErrorUpstream, Status: 503}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	begin := time.Now()
	done := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		done <- callErr
	}()
	// Primary a,b fail fast; the retry subroute must run (with unused it picks
	// c) and win, well before the 5s hedge window.
	primary := map[string]bool{}
	for len(primary) < 2 {
		select {
		case provider := <-started:
			primary[provider] = true
		case <-time.After(time.Second):
			t.Fatalf("primary race did not start: %#v", primary)
		}
	}
	if !primary["a"] || !primary["b"] {
		t.Fatalf("primary must race a,b: %#v", primary)
	}
	select {
	case provider := <-started:
		if provider != "c" {
			t.Fatalf("retry subroute must pick the only unused target c, got %q", provider)
		}
	case <-time.After(time.Second):
		t.Fatal("retry subroute never ran after a fast terminal failure")
	}
	if callErr := <-done; callErr != nil {
		t.Fatalf("route failed: %v", callErr)
	}
	if elapsed := time.Since(begin); elapsed >= time.Second {
		t.Fatalf("route waited for the hedge window: %s", elapsed)
	}
}

// ---------------------------------------------------------------------------
// semaphore bounds
// ---------------------------------------------------------------------------

func TestSemaphoreMaxCallsBoundsTotalCalls(t *testing.T) {
	rules := append(entryRules([]string{"a", "b", "c", "d"}, 2),
		retryRule("standard", "standard.retry", 1),
		semaphoreRule("standard", 3, 3, 1))
	rules = append(rules, retryRulesUnused("standard", []string{"429"}, []string{"a", "b", "c", "d"}, 1)...)
	compiled := rulesConfig(t, 4, rules)
	var mu sync.Mutex
	calls := 0
	executor := &fakeExecutor{do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil {
		t.Fatal("expected failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("semaphore max_calls bound violated: %d calls", calls)
	}
}

func TestSemaphoreMaxCallsPerProviderAcrossRequest(t *testing.T) {
	// The retry subroute without the unused policy would repeat provider a;
	// the request-wide per-provider budget must block the second call.
	rules := append(entryRules([]string{"a"}, 1),
		retryRule("standard", "standard.retry", 2),
		semaphoreRule("standard", 4, 4, 1))
	rules = append(rules, filterError("standard.retry", "429"))
	rules = append(rules, filterProvider("standard.retry", "a"))
	rules = append(rules, mapRule("standard.retry", "native-model"), rankRule("standard.retry"), raceRule("standard.retry", 1))
	compiled := rulesConfig(t, 1, rules)
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorRateLimit {
		t.Fatalf("expected rate limit, got %#v", callErr)
	}
	if calls.Load() != 1 {
		t.Fatalf("max_calls_per_provider bound violated: %d calls", calls.Load())
	}
}

func TestSemaphoreMaxInFlightGatesHedgeTarget(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 2),
		hedgeRule("standard", 30*time.Millisecond, "standard.hedge"),
		semaphoreRule("standard", 3, 2, 1))
	rules = append(rules, filterProviderUnused("standard.hedge", "a", "b", "c"))
	rules = append(rules, mapRule("standard.hedge", "native-model"), rankRule("standard.hedge"), raceRule("standard.hedge", 1))
	compiled := rulesConfig(t, 3, rules)
	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	cStarted := make(chan struct{})
	releaseC := make(chan struct{})
	var once sync.Once
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		switch target.Provider {
		case "a":
			<-releaseA
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		case "b":
			<-releaseB
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		default:
			once.Do(func() { close(cStarted) })
			<-releaseC
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(requestCtx, "standard", ExecuteRequest{Kind: RequestChat})
		done <- callErr
	}()
	// a and b are in flight (maxInFlight=2); the hedge fires and tries c, which
	// must stay blocked until a slot frees.
	select {
	case <-cStarted:
		t.Fatal("hedge target started while maxInFlight was exhausted")
	case <-time.After(120 * time.Millisecond):
	}
	close(releaseA)
	select {
	case <-cStarted:
	case <-time.After(time.Second):
		t.Fatal("hedge target never started after an in-flight slot freed")
	}
	close(releaseC)
	close(releaseB)
	if callErr := <-done; callErr == nil {
		t.Fatal("expected failure")
	}
}

func TestSemaphoreRefillsPartiallyStartedInitialRace(t *testing.T) {
	rules := append(entryRules([]string{"a", "b", "c"}, 3),
		semaphoreRule("standard", 3, 1, 1))
	compiled := rulesConfig(t, 3, rules)
	var mu sync.Mutex
	var called []string
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		called = append(called, target.Provider)
		mu.Unlock()
		if target.Provider == "c" {
			return successBody("c"), nil
		}
		return nil, &CallError{Class: ErrorUpstream, Status: 503}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	body, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("semaphore-limited initial race dropped its successful target: %v", callErr)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "c" {
		t.Fatalf("unexpected winner: %s, %v", body, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(called, ","); got != "a,b,c" {
		t.Fatalf("initial batch was not refilled in rank order: %s", got)
	}
}

func TestNoUpstreamCallsAfterWinner(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 1),
		retryRule("standard", "standard.retry", 3))
	rules = append(rules, filterError("standard.retry", "429", "5xx"))
	rules = append(rules, filterProviderUnused("standard.retry", "a", "b"))
	rules = append(rules, mapRule("standard.retry", "native-model"), rankRule("standard.retry"), raceRule("standard.retry", 1))
	compiled := rulesConfig(t, 2, rules)
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		if target.Provider == "a" {
			return successBody("a"), nil
		}
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatal(callErr)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls continued after the winner: %d", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// streaming
// ---------------------------------------------------------------------------

func TestStreamingWinnerRequiresMeaningfulEvent(t *testing.T) {
	compiled := raceOnlyConfig(t)
	cancelled := make(chan struct{})
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(ctx context.Context, target Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 3)
			go func() {
				defer close(stream)
				stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"role":"assistant"}}]}`)}
				if target.Provider == "a" {
					<-ctx.Done()
					close(cancelled)
					return
				}
				time.Sleep(20 * time.Millisecond)
				stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"content":"hello"}}]}`), Meaningful: true}
			}()
			return stream, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	selected, err := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if err != nil {
		t.Fatal(err)
	}
	defer selected.Cancel()
	if len(selected.Buffered) != 2 || !selected.Buffered[1].Meaningful {
		t.Fatalf("winner buffer does not preserve prelude and meaningful event: %#v", selected.Buffered)
	}
	if selected.Provider != "b" {
		t.Fatalf("winner provider not reported: %q", selected.Provider)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("streaming loser was not cancelled")
	}
}

func TestStreamingRaceIgnoresErrorBeforeWinner(t *testing.T) {
	compiled := raceOnlyConfig(t)
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(_ context.Context, target Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 1)
			if target.Provider == "a" {
				stream <- StreamEvent{Err: &CallError{Class: ErrorUpstream, Status: 503}}
				close(stream)
				return stream, nil
			}
			go func() {
				time.Sleep(10 * time.Millisecond)
				stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"content":"winner"}}]}`), Meaningful: true}
				close(stream)
			}()
			return stream, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	selected, callErr := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("first stream error incorrectly won: %v", callErr)
	}
	defer selected.Cancel()
	if len(selected.Buffered) != 1 || !strings.Contains(string(selected.Buffered[0].Data), "winner") {
		t.Fatalf("unexpected selected stream: %#v", selected.Buffered)
	}
}

func TestSelectedStreamHonorsClientCancellation(t *testing.T) {
	compiled := rulesConfig(t, 1, entryRules([]string{"a"}, 1))
	upstreamCancelled := make(chan struct{})
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(ctx context.Context, _ Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 1)
			stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"content":"hello"}}]}`), Meaningful: true}
			go func() {
				<-ctx.Done()
				close(upstreamCancelled)
				close(stream)
			}()
			return stream, nil
		},
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	runner := newRunner(compiled, newCatalog(compiled), executor)
	selected, callErr := runner.SelectStream(requestCtx, "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatal(callErr)
	}
	cancelRequest()
	defer selected.Cancel()
	select {
	case <-upstreamCancelled:
	case <-time.After(time.Second):
		t.Fatal("client cancellation did not reach selected upstream stream")
	}
}

func TestStreamingRouteTimeoutKeepsWinnerStreamAlive(t *testing.T) {
	compiled := rulesConfig(t, 1, append(entryRules([]string{"a"}, 1),
		timeoutRule("standard", 30*time.Millisecond)))
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(ctx context.Context, _ Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			stream := make(chan StreamEvent, 2)
			go func() {
				defer close(stream)
				stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"content":"hello"}}]}`), Meaningful: true}
				select {
				case <-ctx.Done():
					return
				case <-time.After(60 * time.Millisecond):
					stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`)}
				}
			}()
			return stream, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	selected, callErr := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatal(callErr)
	}
	defer selected.Cancel()
	select {
	case event, open := <-selected.Remaining:
		if !open || !strings.Contains(string(event.Data), `"finish_reason":"stop"`) {
			t.Fatalf("winner stream was cut by the route timeout: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("selected stream did not finish")
	}
}

func TestNonStreamingRouteTimeoutClassified(t *testing.T) {
	compiled := rulesConfig(t, 1, append(entryRules([]string{"a"}, 1),
		timeoutRule("standard", 20*time.Millisecond)))
	executor := &fakeExecutor{do: func(ctx context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
		<-ctx.Done()
		return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorTimeout || callErr.Status != 504 {
		t.Fatalf("route timeout classification mismatch: %#v", callErr)
	}
}

// ---------------------------------------------------------------------------
// cooldown, fallback, meaningful payload
// ---------------------------------------------------------------------------

func TestCooldownSkipsRecentlyFailedProvider(t *testing.T) {
	compiled := raceOnlyConfig(t)
	var mu sync.Mutex
	calls := map[string]int{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
		mu.Unlock()
		if target.Provider == "a" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		time.Sleep(15 * time.Millisecond)
		return successBody("b"), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatal(callErr)
	}
	mu.Lock()
	firstA := calls["a"]
	mu.Unlock()
	if firstA != 1 {
		t.Fatalf("failed provider was not observed before winner: %#v", calls)
	}
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatal(callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"] != firstA || calls["b"] != 2 {
		t.Fatalf("cooldown did not skip failed provider: %#v", calls)
	}
}

func TestFallbackRunsOnlyAfterMatchingFailure(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 2),
		fallbackRules("standard", []string{"5xx"}, []string{"c"})...)
	compiled := rulesConfig(t, 3, rules)
	order := make(chan string, 3)
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		order <- target.Provider
		if target.Provider != "c" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		return successBody("c"), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatalf("fallback failed: %v", callErr)
	}
	first, second, third := <-order, <-order, <-order
	if third != "c" || !((first == "a" && second == "b") || (first == "b" && second == "a")) {
		t.Fatalf("unexpected fallback order: %q, %q, %q", first, second, third)
	}
}

func TestFallback400NotApplicableReturnsOriginal(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 2),
		fallbackRules("standard", []string{"5xx"}, []string{"c"})...)
	compiled := rulesConfig(t, 3, rules)
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, _ Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return nil, &CallError{Class: ErrorInvalid, Status: 400}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorInvalid || callErr.Status != 400 {
		t.Fatalf("original 400 must be returned, got %#v", callErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("fallback fired for a non-applicable failure: %d calls", calls.Load())
	}
}

func TestFallbackSharesPrimarySemaphoreBudget(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 2),
		semaphoreRule("standard", 2, 2, 1))
	rules = append(rules, fallbackRules("standard", []string{"5xx"}, []string{"c"})...)
	compiled := rulesConfig(t, 3, rules)
	var mu sync.Mutex
	calls := map[string]int{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
		mu.Unlock()
		if target.Provider == "c" {
			return successBody("c"), nil
		}
		return nil, &CallError{Class: ErrorUpstream, Status: 503}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr == nil {
		t.Fatal("fallback bypassed exhausted request-wide call budget")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"] != 1 || calls["b"] != 1 || calls["c"] != 0 {
		t.Fatalf("fallback did not share primary semaphore counters: %#v", calls)
	}
}

func TestRouteTimeoutBoundsFallback(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 2),
		timeoutRule("standard", 40*time.Millisecond))
	rules = append(rules, fallbackRules("standard", []string{"5xx"}, []string{"c"})...)
	compiled := rulesConfig(t, 3, rules)
	fallbackCancelled := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		if target.Provider != "c" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		<-ctx.Done()
		close(fallbackCancelled)
		return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	started := time.Now()
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorTimeout {
		t.Fatalf("fallback escaped route timeout: %#v", callErr)
	}
	if elapsed := time.Since(started); elapsed >= 150*time.Millisecond {
		t.Fatalf("fallback exceeded route timeout: %s", elapsed)
	}
	select {
	case <-fallbackCancelled:
	case <-time.After(time.Second):
		t.Fatal("route timeout did not cancel fallback upstream")
	}
}

func TestFailedBatchAggregationIsIndependentOfCompletionOrder(t *testing.T) {
	for _, notFoundLast := range []bool{false, true} {
		name := "not-found-first"
		if notFoundLast {
			name = "not-found-last"
		}
		t.Run(name, func(t *testing.T) {
			rules := append(entryRules([]string{"a", "b", "c"}, 2),
				retryRule("standard", "standard.retry", 1))
			rules = append(rules, retryRulesUnused("standard", []string{"429"}, []string{"a", "b", "c"}, 1)...)
			compiled := rulesConfig(t, 3, rules)
			var calls atomic.Int32
			executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
				calls.Add(1)
				switch target.Provider {
				case "a":
					if notFoundLast {
						time.Sleep(20 * time.Millisecond)
					}
					return nil, &CallError{Class: ErrorNotFound, Status: 404}
				case "b":
					if !notFoundLast {
						time.Sleep(20 * time.Millisecond)
					}
					return nil, &CallError{Class: ErrorRateLimit, Status: 429}
				default:
					return successBody(target.Provider), nil
				}
			}}
			runner := newRunner(compiled, newCatalog(compiled), executor)
			runner.sleep = func(context.Context, time.Duration) error { return nil }
			_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
			if callErr == nil || callErr.Class != ErrorNotFound {
				t.Fatalf("mixed batch produced timing-dependent error: %#v", callErr)
			}
			if calls.Load() != 2 {
				t.Fatalf("retry started despite a non-retryable batch failure: %d calls", calls.Load())
			}
		})
	}
}

func TestStreamingFallbackReturnsCancellationHandle(t *testing.T) {
	rules := append(entryRules([]string{"a", "b"}, 2),
		fallbackRules("standard", []string{"5xx"}, []string{"c"})...)
	compiled := rulesConfig(t, 3, rules)
	upstreamCancelled := make(chan struct{})
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(ctx context.Context, target Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			if target.Provider != "c" {
				return nil, &CallError{Class: ErrorUpstream, Status: 503}
			}
			stream := make(chan StreamEvent, 1)
			stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"content":"fallback"}}]}`), Meaningful: true}
			go func() {
				<-ctx.Done()
				close(upstreamCancelled)
				close(stream)
			}()
			return stream, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	selected, callErr := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("streaming fallback failed: %v", callErr)
	}
	if selected.Cancel == nil || selected.Provider != "c" {
		t.Fatalf("fallback winner has no usable cancellation handle: %#v", selected)
	}
	selected.Cancel()
	select {
	case <-upstreamCancelled:
	case <-time.After(time.Second):
		t.Fatal("fallback cancellation did not reach the selected upstream")
	}
}

func TestMeaningfulPayloadRecognizesReasoningAndTools(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		event string
	}{
		{name: "chat reasoning", data: `{"choices":[{"delta":{"reasoning":"thinking"}}]}`},
		{name: "chat tool", data: `{"choices":[{"delta":{"tool_calls":[{"id":"call"}]}}]}`},
		{name: "responses reasoning", data: `{"delta":"thinking"}`, event: "response.reasoning_summary_text.delta"},
		{name: "responses tool", data: `{"delta":"{\"x\":"}`, event: "response.function_call_arguments.delta"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !meaningfulPayload([]byte(test.data), test.event) {
				t.Fatalf("payload was not meaningful: %s", test.data)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// lease
// ---------------------------------------------------------------------------

func leasePipeline(rules []Rule) []Rule {
	lease := leaseRule("standard", func(r *LeaseRule) {
		r.Source = "winner"
		r.Duration = Duration{time.Minute}
		r.RenewOnSuccess = boolPtr(true)
		r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
	})
	// splice lease between rank and race
	return append(rules[:4], append([]Rule{lease}, rules[4:]...)...)
}

func TestLeasePromotesHolderToFront(t *testing.T) {
	compiled := rulesConfig(t, 3, leasePipeline(entryRules([]string{"a", "b", "c"}, 1)))
	var mu sync.Mutex
	called := []string{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		called = append(called, target.Provider)
		mu.Unlock()
		if target.Provider == "b" {
			return successBody("b"), nil
		}
		return nil, &CallError{Class: ErrorUpstream, Status: 503}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	// b is the low-priority holder; with race count 1 it must be the only
	// target of the batch and win.
	runner.leases.Renew("standard", "b", time.Minute)
	body, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatal(callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "b" {
		t.Fatalf("lease holder did not win: %s", body)
	}
	if len(called) != 1 || called[0] != "b" {
		t.Fatalf("lease holder was not promoted to the front: %#v", called)
	}
}

func TestLeaseAcquiredByWinnerAndReleasedOnHardFailure(t *testing.T) {
	compiled := rulesConfig(t, 3, leasePipeline(entryRules([]string{"a", "b", "c"}, 2)))
	var failAll atomic.Bool
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		if failAll.Load() {
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		}
		if target.Provider == "a" {
			return successBody("a"), nil
		}
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatal(callErr)
	}
	if holder, ok := runner.leases.Holder("standard"); !ok || holder != "a" {
		t.Fatalf("winner did not acquire the lease: %q %v", holder, ok)
	}
	failAll.Store(true)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr == nil {
		t.Fatal("expected failure")
	}
	if _, ok := runner.leases.Holder("standard"); ok {
		t.Fatal("lease was not released on hard failure of the holder")
	}
}

func TestLeaseLoserCancellationIsNeutral(t *testing.T) {
	compiled := rulesConfig(t, 2, leasePipeline(entryRules([]string{"a", "b"}, 2)))
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		if target.Provider == "b" {
			return nil, &CallError{Class: ErrorCancelled, Status: 499}
		}
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.leases.Renew("standard", "a", time.Minute)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatal(callErr)
	}
	if holder, ok := runner.leases.Holder("standard"); !ok || holder != "a" {
		t.Fatalf("cancelled loser affected the lease: %q %v", holder, ok)
	}
}

func TestLeaseReleasedAfterConsecutiveSlowStarts(t *testing.T) {
	lease := leaseRule("standard", func(r *LeaseRule) {
		r.Source = "winner"
		r.Duration = Duration{time.Minute}
		r.RenewOnSuccess = boolPtr(true)
		r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
		r.ReleaseAfterSlowStarts = 2
		r.SlowStart = Duration{30 * time.Millisecond}
	})
	rules := entryRules([]string{"a", "b"}, 1)
	rules = append(rules[:4], append([]Rule{lease}, rules[4:]...)...)
	compiled := rulesConfig(t, 2, rules)
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		if target.Provider == "a" {
			time.Sleep(60 * time.Millisecond) // slow winner
		}
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.leases.Renew("standard", "a", time.Minute)
	for i := 0; i < 2; i++ {
		if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
			t.Fatal(callErr)
		}
	}
	if _, ok := runner.leases.Holder("standard"); ok {
		t.Fatal("lease survived two consecutive slow starts")
	}
	if count := runner.leases.HolderCount(); count != 0 {
		t.Fatalf("expected no live leases, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// affinity pinning
// ---------------------------------------------------------------------------

func affinityPipeline(t *testing.T) *compiledConfig {
	t.Helper()
	affinity := affinityRule("standard", func(r *AffinityRule) {
		r.Sources = []string{"responses.previous_response_id"}
		r.TTL = Duration{time.Hour}
		r.OnMissing = "ignore"
		r.OnProviderFailure = "fail-closed"
	})
	rules := entryRules([]string{"a", "b"}, 2)
	rules = append(rules[:4], append([]Rule{affinity}, rules[4:]...)...)
	return rulesConfig(t, 2, rules)
}

func responsesRequest(previousResponseID string) ExecuteRequest {
	body := []byte(`{"model":"standard","previous_response_id":"` + previousResponseID + `","input":"hi"}`)
	return ExecuteRequest{Kind: RequestResponses, Body: body}
}

func TestAffinityPinsRouteAndFailsClosed(t *testing.T) {
	compiled := affinityPipeline(t)
	var recover atomic.Bool
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		if !recover.Load() && target.Provider == "a" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.affinity.Bind("resp_1", "a", time.Hour)
	_, callErr := runner.Run(context.Background(), "standard", responsesRequest("resp_1"))
	if callErr == nil {
		t.Fatal("known affinity route must fail closed when the pinned provider fails")
	}
	recover.Store(true)
	body, callErr := runner.Run(context.Background(), "standard", responsesRequest("resp_1"))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "a" {
		t.Fatalf("pinned provider did not win: %s", body)
	}
}

func TestAffinityUnknownIdRoutesNormally(t *testing.T) {
	compiled := affinityPipeline(t)
	started := make(chan string, 2)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		select {
		case <-release:
			if target.Provider == "b" {
				return successBody("b"), nil
			}
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		case <-ctx.Done():
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	result := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", responsesRequest("unknown-id"))
		result <- callErr
	}()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case provider := <-started:
			seen[provider] = true
		case <-time.After(time.Second):
			t.Fatalf("unknown affinity id must not narrow the route: %#v", seen)
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("unknown affinity id must not narrow the route: %#v", seen)
	}
	close(release)
	if callErr := <-result; callErr != nil {
		t.Fatalf("unknown affinity request failed: %v", callErr)
	}
}

func TestChatNeverUsesAffinity(t *testing.T) {
	compiled := affinityPipeline(t)
	started := make(chan string, 2)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		select {
		case <-release:
			if target.Provider == "b" {
				return successBody("b"), nil
			}
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		case <-ctx.Done():
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.affinity.Bind("resp_1", "a", time.Hour)
	chat := ExecuteRequest{Kind: RequestChat, Body: []byte(`{"model":"standard","previous_response_id":"resp_1","messages":[{"role":"user","content":"hi"}]}`)}
	result := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", chat)
		result <- callErr
	}()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case provider := <-started:
			seen[provider] = true
		case <-time.After(time.Second):
			t.Fatalf("chat route was narrowed despite no affinity support: %#v", seen)
		}
	}
	close(release)
	if callErr := <-result; callErr != nil {
		t.Fatalf("chat request failed: %v", callErr)
	}
}

func TestAffinityPinnedRouteNeverUsesFallback(t *testing.T) {
	// Known affinity narrows the route to the pinned provider; the fallback
	// subroute must not pick up the stateful chain (no state replay).
	affinity := affinityRule("standard", func(r *AffinityRule) {
		r.Sources = []string{"responses.previous_response_id"}
		r.TTL = Duration{time.Hour}
		r.OnMissing = "ignore"
		r.OnProviderFailure = "fail-closed"
	})
	rules := entryRules([]string{"a", "b"}, 2)
	rules = append(rules[:4], append([]Rule{affinity}, rules[4:]...)...)
	rules = append(rules, fallbackRules("standard", []string{"5xx", "timeout"}, []string{"c"})...)
	compiled := rulesConfig(t, 3, rules)
	var mu sync.Mutex
	called := map[string]int{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		called[target.Provider]++
		mu.Unlock()
		if target.Provider == "a" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.affinity.Bind("resp_1", "a", time.Hour)
	_, callErr := runner.Run(context.Background(), "standard", responsesRequest("resp_1"))
	if callErr == nil {
		t.Fatal("pinned provider failure must fail closed and skip the fallback")
	}
	mu.Lock()
	defer mu.Unlock()
	if called["c"] != 0 {
		t.Fatalf("fallback ran for a pinned route: backup provider called %d times", called["c"])
	}
	if called["b"] != 0 {
		t.Fatalf("pinned route reached a non-pinned provider b: %d calls", called["b"])
	}
}

func TestAffinityResolutionFailureRemainsPinned(t *testing.T) {
	cfg := testConfig()
	// An explicit catalog requires a last-known-good snapshot. Leaving the
	// fresh catalog empty makes target resolution fail before executor dispatch.
	cfg.Providers[0].ModelsURL = "https://catalog.invalid/v1/models"
	affinity := affinityRule("standard", func(r *AffinityRule) {
		r.Sources = []string{"responses.previous_response_id"}
		r.TTL = Duration{time.Hour}
		r.OnMissing = "ignore"
		r.OnProviderFailure = "fail-closed"
	})
	rules := entryRules([]string{"a", "b"}, 2)
	rules = append(rules[:4], append([]Rule{affinity}, rules[4:]...)...)
	cfg.RoutingRules = rules
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.affinity.Bind("resp_1", "a", time.Hour)
	if _, callErr := runner.Run(context.Background(), "standard", responsesRequest("resp_1")); callErr == nil {
		t.Fatal("pinned resolution failure incorrectly opened the fallback route")
	}
	if calls.Load() != 0 {
		t.Fatalf("resolution failure dispatched %d upstream calls", calls.Load())
	}
	if provider, ok := runner.affinity.Lookup("resp_1"); !ok || provider != "a" {
		t.Fatalf("runtime resolution failure removed affinity mapping: %q %v", provider, ok)
	}
}

func TestAffinityStaleMappingFallsBackToPool(t *testing.T) {
	compiled := affinityPipeline(t)
	started := make(chan string, 2)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		select {
		case <-release:
			if target.Provider == "b" {
				return successBody("b"), nil
			}
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		case <-ctx.Done():
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	// Mapping to a provider that no longer exists in the config must not fail
	// the request: it is dropped and the request routes normally.
	runner.affinity.Bind("resp_1", "ghost", time.Hour)
	result := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", responsesRequest("resp_1"))
		result <- callErr
	}()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case provider := <-started:
			seen[provider] = true
		case <-time.After(time.Second):
			t.Fatalf("stale mapping did not fall back to the pool: %#v", seen)
		}
	}
	close(release)
	if callErr := <-result; callErr != nil {
		t.Fatalf("stale affinity mapping failed the request: %v", callErr)
	}
	if _, ok := runner.affinity.Lookup("resp_1"); ok {
		t.Fatal("stale mapping was not dropped")
	}
}

// ---------------------------------------------------------------------------
// f7-10: route-native-provider mapping (map action, exact catalogs, fallback
// through model_not_found, and per-provider native aliases).
// ---------------------------------------------------------------------------

// catalogServer returns a provider-scoped catalog serving the given model ids.
func catalogServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"object":"list","data":[%s]}`, func() string {
			items := make([]string, 0, len(ids))
			for _, id := range ids {
				items = append(items, `{"id":"`+id+`"}`)
			}
			return strings.Join(items, ",")
		}())
	}))
	return server
}

// compiledWithCatalogs builds a compiled config whose catalog sources are set
// directly so tests can prove exact native validation without real HTTP. It
// returns the shared catalog instance so the runner uses the same snapshots.
func compiledWithCatalogs(t *testing.T, rules []Rule, catalogs map[string][]string) (*compiledConfig, *Catalog) {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = []Provider{
		{ID: "a", BaseProvider: "openai", InferenceURL: "https://a.invalid", Priority: 20},
		{ID: "b", BaseProvider: "openai", InferenceURL: "https://b.invalid", Priority: 10},
		{ID: "c", BaseProvider: "openai", InferenceURL: "https://c.invalid", Priority: 5},
	}
	cfg.RoutingRules = rules
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	compiled.catalogSources = make(map[string][]catalogSource, len(catalogs))
	for providerID, ids := range catalogs {
		server := catalogServer(t, ids...)
		t.Cleanup(server.Close)
		compiled.catalogSources[providerID] = []catalogSource{{URL: server.URL, Explicit: true}}
	}
	catalog := newCatalog(compiled)
	if failures := catalog.Refresh(context.Background()); len(failures) != 0 {
		t.Fatalf("catalog refresh failed: %v", failures)
	}
	return compiled, catalog
}

func TestMapGivesDifferentProvidersDifferentNatives(t *testing.T) {
	rules := []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-a"),
		filterProvider("standard", "b"),
		mapRule("standard", "native-b"),
		rankRule("standard"),
		raceRule("standard", 2),
	}
	compiled, catalog := compiledWithCatalogs(t, rules, map[string][]string{"a": {"native-a"}, "b": {"native-b"}})
	called := make(chan Target, 2)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		called <- target
		<-release
		if target.Provider == "a" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, catalog, executor)
	result := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		result <- callErr
	}()
	dispatched := map[string]string{}
	for len(dispatched) < 2 {
		select {
		case target := <-called:
			dispatched[target.Provider] = target.Model
		case <-time.After(time.Second):
			t.Fatalf("both mapped providers did not dispatch: %#v", dispatched)
		}
	}
	if dispatched["a"] != "native-a" || dispatched["b"] != "native-b" {
		t.Fatalf("each provider must receive its own mapped native: %#v", dispatched)
	}
	close(release)
	if callErr := <-result; callErr != nil {
		t.Fatalf("route failed: %v", callErr)
	}
}

func TestModelNotFoundTriggersExplicitFallback(t *testing.T) {
	// Provider a's snapshot lacks the primary alias but carries the fallback
	// alias; provider b lacks both. All primary targets locally fail
	// model_not_found, which must activate the fallback subroute and dispatch
	// its target with the subroute's own native.
	compiled, catalog := compiledWithCatalogs(t, []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "alias-a"),
		rankRule("standard"),
		raceRule("standard", 2),
		fallbackRule("standard", "standard.fallback"),
		filterError("standard.fallback", "model_not_found"),
		filterProvider("standard.fallback", "a"),
		mapRule("standard.fallback", "alias-b"),
		rankRule("standard.fallback"),
		raceRule("standard.fallback", 1),
	}, map[string][]string{"a": {"alias-b"}, "b": {"unrelated"}})
	var mu sync.Mutex
	var called []Target
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		called = append(called, target)
		mu.Unlock()
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, catalog, executor)
	body, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("fallback did not recover: %v", callErr)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "a" {
		t.Fatalf("fallback winner mismatch: %s", body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(called) != 1 {
		t.Fatalf("expected exactly one upstream call (fallback a with alias-b), got %#v", called)
	}
	if got := called[0]; got.Provider != "a" || got.Model != "alias-b" {
		t.Fatalf("fallback must use the mapped native of its own route: %#v", got)
	}
}

func TestModelNotFoundWithMatchingRetryStaysBounded(t *testing.T) {
	// All primary targets fail model_not_found and the retry subroute opts in:
	// the unused provider policy keeps the calls bounded and no provider is
	// ever dispatched twice.
	rules := append(entryRules([]string{"a", "b", "c"}, 2),
		retryRule("standard", "standard.retry", 1),
		semaphoreRule("standard", 4, 3, 1))
	rules = append(rules, retryRulesUnused("standard", []string{"model_not_found"}, []string{"a", "b", "c"}, 1)...)
	compiled, catalog := compiledWithCatalogs(t, rules, map[string][]string{"a": {"other"}, "b": {"other"}, "c": {"other"}})
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, catalog, executor)
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorModelNotFound {
		t.Fatalf("expected model_not_found after bounded retry, got %v", callErr)
	}
	if calls.Load() != 0 {
		t.Fatalf("catalog-rejected targets were dispatched upstream: %d calls", calls.Load())
	}
}

func TestModelNotFoundNeverSelectsUnrelatedModel(t *testing.T) {
	compiled, catalog := compiledWithCatalogs(t, entryRules([]string{"a"}, 1), map[string][]string{"a": {"alpha", "zeta"}})
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, catalog, executor)
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorModelNotFound {
		t.Fatalf("expected model_not_found, got %v (never substitute alpha/zeta)", callErr)
	}
	if calls.Load() != 0 {
		t.Fatalf("unrelated model was dispatched to upstream: %d calls", calls.Load())
	}
}

func TestDualAliasFallbackUsesSameProviderOnce(t *testing.T) {
	// Hyperfusion-style scenario: the provider appears in the entry route with
	// one alias and in the fallback subroute with a second alias. When the
	// entry alias is locally absent, the fallback reaches the same provider
	// once with its other native; the per-provider budget is not burned by the
	// pre-validated entry target.
	rules := append(entryRules([]string{"a", "b"}, 2),
		semaphoreRule("standard", 4, 3, 1),
		fallbackRule("standard", "standard.fallback"),
		filterError("standard.fallback", "model_not_found", "5xx"),
		filterProvider("standard.fallback", "a"),
		mapRule("standard.fallback", "gonka/deepseek-ai/DeepSeek-V4"),
		rankRule("standard.fallback"),
		raceRule("standard.fallback", 1),
	)
	compiled, catalog := compiledWithCatalogs(t, rules, map[string][]string{
		"a": {"gonka/deepseek-ai/DeepSeek-V4"},
		"b": {"deepseek-ai/DeepSeek-V3"},
	})
	var mu sync.Mutex
	var called []Target
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		called = append(called, target)
		mu.Unlock()
		return successBody(target.Provider), nil
	}}
	runner := newRunner(compiled, catalog, executor)
	body, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("dual-alias fallback failed: %v", callErr)
	}
	mu.Lock()
	if len(called) != 1 {
		t.Fatalf("expected exactly one upstream call, got %#v", called)
	}
	if got := called[0]; got.Provider != "a" || got.Model != "gonka/deepseek-ai/DeepSeek-V4" {
		t.Fatalf("fallback must use the mapped native of its own stage once: %#v", got)
	}
	mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "a" {
		t.Fatalf("fallback winner mismatch: %s", body)
	}
}

// ---------------------------------------------------------------------------
// nested explicit transitions
// ---------------------------------------------------------------------------

func TestNestedTransitionStandardFallbackThenRetry(t *testing.T) {
	// standard → standard.fallback → standard.fallback.retry is just a graph of
	// named routes compiled by the same mechanism; the fallback subroute has
	// its own retry target.
	var order []string
	rules := append(entryRules([]string{"a", "b"}, 2),
		fallbackRule("standard", "standard.fallback"),
		// fallback subroute
		filterError("standard.fallback", "5xx"),
		filterProvider("standard.fallback", "c"),
		mapRule("standard.fallback", "native-model"),
		rankRule("standard.fallback"),
		raceRule("standard.fallback", 1),
		// fallback subroute retries the same provider without the unused policy
		retryRule("standard.fallback", "standard.fallback.retry", 1),
		filterError("standard.fallback.retry", "5xx"),
		filterProvider("standard.fallback.retry", "c"),
		mapRule("standard.fallback.retry", "native-model"),
		rankRule("standard.fallback.retry"),
		raceRule("standard.fallback.retry", 1),
	)
	compiled := rulesConfig(t, 3, rules)
	var mu sync.Mutex
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		order = append(order, target.Provider)
		mu.Unlock()
		switch target.Provider {
		case "a", "b":
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		case "c":
			if len(order) < 4 {
				return nil, &CallError{Class: ErrorUpstream, Status: 503}
			}
			return successBody("c"), nil
		}
		return nil, &CallError{Class: ErrorInvalid, Status: 500}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	body, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("nested fallback→retry did not recover: %v", callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 4 || order[2] != "c" || order[3] != "c" {
		t.Fatalf("unexpected nested transition order: %#v", order)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "c" {
		t.Fatalf("unexpected winner: %s", body)
	}
}
