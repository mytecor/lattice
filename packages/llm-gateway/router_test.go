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

func raceOnlyConfig(t *testing.T) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.RoutingRules = cfg.RoutingRules[:3]
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

// rulesConfig returns a compiled config whose pool contains n providers of the
// "group" access group, ranked by descending priority (a first).
func rulesConfig(t *testing.T, n int, rules []RoutingRule) *compiledConfig {
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
	compiled := rulesConfig(t, 3, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
	})
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
// retry scope
// ---------------------------------------------------------------------------

func TestRetryNextUsesOnlyUnusedTargetsWithinBudget(t *testing.T) {
	compiled := rulesConfig(t, 3, []RoutingRule{
		mapRule("standard", "native-model", "a", "b", "c"), rankRule("standard"), raceRule("standard", 2),
		retryNextRule("standard", 1, 2),
	})
	var mu sync.Mutex
	calls := map[string]int{}
	order := []string{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
		order = append(order, target.Provider)
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
		t.Fatalf("retry next did not recover: %v", callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("expected one initial batch plus one next target, got %d calls: %#v", len(order), order)
	}
	for provider, count := range calls {
		if count > 1 {
			t.Fatalf("scope=next reused provider %q %d times", provider, count)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload["winner"] != "c" {
		t.Fatalf("next target did not win: %s", body)
	}
}

func TestRetrySameRepeatsOriginalSelection(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"),
		rankRule("standard"),
		raceRule("standard", 2),
		rule("retry", "standard", func(r *RoutingRule) {
			// Legacy: no scope => "same".
			r.Attempts = 1
			r.On = []string{"429", "5xx"}
			r.Backoff = &BackoffConfig{Initial: Duration{time.Millisecond}, Max: Duration{time.Millisecond}}
		}),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 4)
	release := make(chan struct{})
	var attempt atomic.Int32
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		if attempt.Add(1) <= 2 {
			return nil, &CallError{Class: ErrorUpstream, Status: 503} // initial batch fails
		}
		select {
		case <-release:
			return successBody(target.Provider), nil
		case <-ctx.Done():
			return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	result := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		result <- callErr
	}()
	// The whole selection must be retried with the same providers.
	seen := map[string]int{}
	for total := 0; total < 4; {
		select {
		case provider := <-started:
			seen[provider]++
			total++
		case <-time.After(time.Second):
			t.Fatalf("scope=same did not repeat the selection: %#v", seen)
		}
	}
	seenTotal := seen["a"] + seen["b"]
	if seenTotal != 4 || seen["a"] < 2 || seen["b"] < 2 {
		t.Fatalf("scope=same must repeat the whole selection: %#v", seen)
	}
	close(release)
	if callErr := <-result; callErr != nil {
		t.Fatalf("scope=same did not recover: %v", callErr)
	}
}

func TestRetryNextPoolExhaustionReturnsLastError(t *testing.T) {
	compiled := rulesConfig(t, 2, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
		retryNextRule("standard", 1, 5), // more batches than remaining targets
	})
	var mu sync.Mutex
	calls := map[string]int{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
		mu.Unlock()
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr == nil || callErr.Class != ErrorRateLimit {
		t.Fatalf("expected rate limit after pool exhaustion, got %v", callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"]+calls["b"] != 2 {
		t.Fatalf("pool exhaustion must not repeat used providers: %#v", calls)
	}
}

func TestRouteTimeoutInterruptsRetryBackoff(t *testing.T) {
	compiled := rulesConfig(t, 2, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 1),
		rule("retry", "standard", func(r *RoutingRule) {
			r.Scope = "next"
			r.Count = 1
			r.Attempts = 1
			r.On = []string{"429"}
			r.Backoff = &BackoffConfig{
				Type: "constant", Initial: Duration{200 * time.Millisecond}, Max: Duration{200 * time.Millisecond},
			}
		}),
		rule("timeout", "standard", func(r *RoutingRule) { r.Duration = Duration{30 * time.Millisecond} }),
	})
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

func TestHedgeStartsOnlyNextBatchAfterDelay(t *testing.T) {
	compiled := rulesConfig(t, 3, []RoutingRule{
		mapRule("standard", "native-model", "a", "b", "c"), rankRule("standard"), raceRule("standard", 2),
		retryNextRule("standard", 1, 2),
		rule("hedge", "standard", func(r *RoutingRule) { r.After = Duration{40 * time.Millisecond} }),
	})
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
	// The next batch (one unused target) must start after the hedge delay while
	// the first two branches are still running.
	select {
	case <-started:
		startTimes = append(startTimes, time.Now())
	case <-time.After(time.Second):
		t.Fatalf("hedged next batch never started")
	}
	if elapsed := startTimes[2].Sub(startTimes[1]); elapsed < 25*time.Millisecond {
		t.Fatalf("next batch started before the hedge delay: %s", elapsed)
	}
	// Hedge must not clone the pool: only one additional target appears.
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

func TestHedgeNewWaveDespiteCooledProvider(t *testing.T) {
	// A whole-batch retryable failure continues the route before the hedge
	// timer, and the next hedge wave uses only unused targets.
	compiled := rulesConfig(t, 3, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 1),
		retryNextRule("standard", 1, 2),
		rule("hedge", "standard", func(r *RoutingRule) { r.After = Duration{50 * time.Millisecond} }),
	})
	started := make(chan string, 4)
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		if target.Provider == "b" {
			return successBody("b"), nil
		}
		return nil, &CallError{Class: ErrorUpstream, Status: 503}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	done := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		done <- callErr
	}()
	first := <-started
	if first != "a" {
		t.Fatalf("initial race must pick the top target, got %q", first)
	}
	second := <-started
	if second != "b" {
		t.Fatalf("next batch must use the next unused target, got %q", second)
	}
	if callErr := <-done; callErr != nil {
		t.Fatalf("route failed: %v", callErr)
	}
}

// ---------------------------------------------------------------------------
// semaphore bounds
// ---------------------------------------------------------------------------

func TestSemaphoreMaxCallsBoundsTotalCalls(t *testing.T) {
	compiled := rulesConfig(t, 4, []RoutingRule{
		mapRule("standard", "native-model", "a", "b", "c", "d"), rankRule("standard"), raceRule("standard", 2),
		retryNextRule("standard", 1, 6),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 3
			r.MaxInFlight = 3
			r.MaxCallsPerProvider = 1
		}),
	})
	var mu sync.Mutex
	calls := 0
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
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

func TestSemaphoreMaxCallsPerProviderEvenForScopeSame(t *testing.T) {
	compiled := rulesConfig(t, 2, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
		rule("retry", "standard", func(r *RoutingRule) {
			r.Scope = "same"
			r.Attempts = 3
			r.On = []string{"429", "5xx"}
		}),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 4
			r.MaxInFlight = 4
			r.MaxCallsPerProvider = 1
		}),
	})
	var mu sync.Mutex
	calls := map[string]int{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
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
	if calls["a"] > 1 || calls["b"] > 1 {
		t.Fatalf("max_calls_per_provider bound violated: %#v", calls)
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
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []RoutingRule{
		mapRule("standard", "native-model", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []RoutingRule{
		mapRule("standard", "native-model", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
		rule("timeout", "standard", func(r *RoutingRule) { r.Duration = Duration{30 * time.Millisecond} }),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules = []RoutingRule{
		mapRule("standard", "native-model", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
		rule("timeout", "standard", func(r *RoutingRule) { r.Duration = Duration{20 * time.Millisecond} }),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
	cfg := testConfig()
	// map(group), rank, race plus the compiled fallback stage (map(backup), fallback).
	cfg.RoutingRules = []RoutingRule{cfg.RoutingRules[0], cfg.RoutingRules[1], cfg.RoutingRules[2], cfg.RoutingRules[4], cfg.RoutingRules[5]}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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

func TestFallbackSharesPrimarySemaphoreBudget(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 2
			r.MaxInFlight = 2
			r.MaxCallsPerProvider = 1
		}),
		poolRule("standard", "backup"),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.On = []string{"5xx"}
			r.FallbackStrategy = "serial"
		}),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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

func TestRouteTimeoutBoundsSerialFallback(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
		rule("timeout", "standard", func(r *RoutingRule) { r.Duration = Duration{40 * time.Millisecond} }),
		poolRule("standard", "backup"),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.On = []string{"5xx"}
			r.FallbackStrategy = "serial"
		}),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("serial fallback escaped route timeout: %#v", callErr)
	}
	if elapsed := time.Since(started); elapsed >= 150*time.Millisecond {
		t.Fatalf("serial fallback exceeded route timeout: %s", elapsed)
	}
	select {
	case <-fallbackCancelled:
	case <-time.After(time.Second):
		t.Fatal("route timeout did not cancel serial fallback upstream")
	}
}

func TestFailedBatchAggregationIsIndependentOfCompletionOrder(t *testing.T) {
	for _, notFoundLast := range []bool{false, true} {
		name := "not-found-first"
		if notFoundLast {
			name = "not-found-last"
		}
		t.Run(name, func(t *testing.T) {
			compiled := rulesConfig(t, 3, []RoutingRule{
				poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
				retryNextRule("standard", 1, 1),
			})
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

func TestSerialStreamingFallbackReturnsCancellationHandle(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 2),
		poolRule("standard", "backup"),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.On = []string{"5xx"}
			r.FallbackStrategy = "serial"
		}),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("serial streaming fallback failed: %v", callErr)
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

func TestLeasePromotesHolderToFront(t *testing.T) {
	compiled := rulesConfig(t, 3, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"),
		rule("lease", "standard", func(r *RoutingRule) {
			r.Source = "winner"
			r.Duration = Duration{time.Minute}
			r.RenewOnSuccess = boolPtr(true)
			r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
		}),
		raceRule("standard", 1),
	})
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
	// target of the batch and win without a retry batch.
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
	compiled := rulesConfig(t, 3, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"),
		rule("lease", "standard", func(r *RoutingRule) {
			r.Source = "winner"
			r.Duration = Duration{time.Minute}
			r.RenewOnSuccess = boolPtr(true)
			r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
		}),
		raceRule("standard", 2),
	})
	// The behavior switch is atomic because cancelled branch goroutines may
	// still be draining after Run returns.
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
	// The holder fails hard and there is no winner: the lease must drop.
	failAll.Store(true)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr == nil {
		t.Fatal("expected failure")
	}
	if _, ok := runner.leases.Holder("standard"); ok {
		t.Fatal("lease was not released on hard failure of the holder")
	}
}

func TestLeaseLoserCancellationIsNeutral(t *testing.T) {
	compiled := rulesConfig(t, 2, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"),
		rule("lease", "standard", func(r *RoutingRule) {
			r.Source = "winner"
			r.Duration = Duration{time.Minute}
			r.RenewOnSuccess = boolPtr(true)
			r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
		}),
		raceRule("standard", 2),
	})
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
	compiled := rulesConfig(t, 2, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"),
		rule("lease", "standard", func(r *RoutingRule) {
			r.Source = "winner"
			r.Duration = Duration{time.Minute}
			r.RenewOnSuccess = boolPtr(true)
			r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
			r.ReleaseAfterSlowStarts = 2
			r.SlowStart = Duration{30 * time.Millisecond}
		}),
		raceRule("standard", 1),
	})
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
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"),
		rankRule("standard"),
		rule("affinity", "standard", func(r *RoutingRule) {
			r.Sources = []string{"responses.previous_response_id"}
			r.TTL = Duration{time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
		raceRule("standard", 2),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func responsesRequest(previousResponseID string) ExecuteRequest {
	body := []byte(`{"model":"standard","previous_response_id":"` + previousResponseID + `","input":"hi"}`)
	return ExecuteRequest{Kind: RequestResponses, Body: body}
}

func TestAffinityPinsRouteAndFailsClosed(t *testing.T) {
	compiled := affinityPipeline(t)
	// A second bound provider must never be reached. The behavior switch is
	// atomic because cancelled branch goroutines may still be draining.
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
	// Both pool members must be eligible: the unknown id must not narrow the route.
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
	// Chat must keep the full pool: no affinity narrowing.
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

func TestSemaphoreMaxInFlightGatesNextBatch(t *testing.T) {
	compiled := rulesConfig(t, 3, []RoutingRule{
		mapRule("standard", "native-model", "a", "b", "c"), rankRule("standard"), raceRule("standard", 2),
		retryNextRule("standard", 1, 2),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 3
			r.MaxInFlight = 2
			r.MaxCallsPerProvider = 1
		}),
	})
	releaseB := make(chan struct{})
	cStarted := make(chan struct{})
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		if target.Provider == "b" {
			<-releaseB
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		}
		if target.Provider == "c" {
			close(cStarted)
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		}
		return nil, &CallError{Class: ErrorRateLimit, Status: 429}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	result := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		result <- callErr
	}()
	// With maxInFlight=2 and an active b, the next batch must not start until a
	// slot frees; the scheduler must not hang or launch c early.
	select {
	case <-cStarted:
		t.Fatal("next batch started while maxInFlight was exhausted")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseB)
	select {
	case <-cStarted:
	case <-time.After(time.Second):
		t.Fatal("next batch never started after an in-flight slot freed")
	}
	if callErr := <-result; callErr == nil {
		t.Fatal("expected failure")
	}
}

func TestSemaphoreRefillsPartiallyStartedInitialRace(t *testing.T) {
	compiled := rulesConfig(t, 3, []RoutingRule{
		mapRule("standard", "native-model", "a", "b", "c"), rankRule("standard"), raceRule("standard", 3),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 3
			r.MaxInFlight = 1
			r.MaxCallsPerProvider = 1
		}),
	})
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
	compiled := rulesConfig(t, 2, []RoutingRule{
		poolRule("standard", "group"), rankRule("standard"), raceRule("standard", 1),
		rule("retry", "standard", func(r *RoutingRule) {
			r.Scope = "same"
			r.Attempts = 3
			r.On = []string{"429", "5xx"}
		}),
	})
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
// affinity fail-closed guarantees
// ---------------------------------------------------------------------------

func TestAffinityPinnedRouteNeverUsesFallback(t *testing.T) {
	// Known affinity narrows the route to the pinned provider; a legacy
	// fallback group must not pick up the stateful chain (no state replay).
	cfg := testConfig()
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"),
		rankRule("standard"),
		rule("affinity", "standard", func(r *RoutingRule) {
			r.Sources = []string{"responses.previous_response_id"}
			r.TTL = Duration{time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
		raceRule("standard", 2),
		poolRule("standard", "backup"),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.On = []string{"5xx", "timeout"}
			r.FallbackStrategy = "serial"
		}),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
	cfg.RoutingRules = []RoutingRule{
		poolRule("standard", "group"),
		rankRule("standard"),
		rule("affinity", "standard", func(r *RoutingRule) {
			r.Sources = []string{"responses.previous_response_id"}
			r.TTL = Duration{time.Hour}
			r.OnMissing = "ignore"
			r.OnProviderFailure = "fail-closed"
		}),
		raceRule("standard", 2),
		poolRule("standard", "backup"),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.On = []string{"invalid_response"}
			r.FallbackStrategy = "serial"
		}),
	}
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
// partial next batch under semaphore pressure
// ---------------------------------------------------------------------------

func TestHedgeRetriesPermitBlockedBatchMembers(t *testing.T) {
	compiled := rulesConfig(t, 4, []RoutingRule{
		mapRule("standard", "native-model", "a", "b", "c", "d"), rankRule("standard"), raceRule("standard", 2),
		retryNextRule("standard", 2, 1),
		rule("hedge", "standard", func(r *RoutingRule) { r.After = Duration{30 * time.Millisecond} }),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 4
			r.MaxInFlight = 3
			r.MaxCallsPerProvider = 1
		}),
	})
	cStarted := make(chan struct{})
	dStarted := make(chan struct{})
	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	releaseC := make(chan struct{})
	var calls atomic.Int32
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		calls.Add(1)
		switch target.Provider {
		case "a":
			<-releaseA
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		case "b":
			<-releaseB
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		case "c":
			close(cStarted)
			<-releaseC
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		default: // d
			close(dStarted)
			return nil, &CallError{Class: ErrorRateLimit, Status: 429}
		}
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	done := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		done <- callErr
	}()
	// First hedge wave starts c; with a and b still in flight (maxInFlight=3)
	// the second member d of the next batch is denied a permit. c stays in
	// flight so the slot genuinely never frees.
	select {
	case <-cStarted:
	case <-time.After(time.Second):
		t.Fatal("first hedge wave did not start c")
	}
	// While a, b and c are all in flight, d must stay blocked across successive
	// hedge waves.
	select {
	case <-dStarted:
		t.Fatal("d started while maxInFlight was exhausted")
	case <-time.After(100 * time.Millisecond):
	}
	// Free a slot by completing c; the next hedge wave must then start d
	// instead of dropping the denied member permanently.
	close(releaseC)
	select {
	case <-dStarted:
	case <-time.After(time.Second):
		t.Fatal("permit-blocked batch member d was never re-launched after a slot freed")
	}
	close(releaseB)
	close(releaseA)
	if callErr := <-done; callErr == nil {
		t.Fatal("expected failure")
	}
	if n := calls.Load(); n != 4 {
		t.Fatalf("semaphore budget violated: %d calls", n)
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
func compiledWithCatalogs(t *testing.T, rules []RoutingRule, catalogs map[string][]string) (*compiledConfig, *Catalog) {
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
	// Replace every catalog source Keyed by provider with the test servers.
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
	compiled, catalog := compiledWithCatalogs(t, []RoutingRule{
		mapRule("standard", "native-a", "a"),
		mapRule("standard", "native-b", "b"),
		rankRule("standard"),
		raceRule("standard", 2),
	}, map[string][]string{"a": {"native-a"}, "b": {"native-b"}})
	called := make(chan Target, 2)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		// Record the dispatched target before any winner can cancel the loser,
		// so the per-provider native mapping is asserted deterministically.
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
	// model_not_found, which must activate the configured fallback stage and
	// dispatch the fallback target with its own native, never an unrelated one.
	compiled, catalog := compiledWithCatalogs(t, []RoutingRule{
		mapRule("standard", "alias-a", "a", "b"),
		rankRule("standard"),
		raceRule("standard", 2),
		mapRule("standard", "alias-b", "a"),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.On = []string{"model_not_found"}
			r.FallbackStrategy = "race"
		}),
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
		t.Fatalf("fallback must use the mapped native of its stage: %#v", got)
	}
}

func TestModelNotFoundWithMatchingRetryStaysBounded(t *testing.T) {
	// All primary targets fail model_not_found and retry.on opts in: the next
	// retry wave uses only unused targets, stays inside the semaphore budget
	// and never dispatches a provider twice.
	compiled, catalog := compiledWithCatalogs(t, []RoutingRule{
		mapRule("standard", "alias-a", "a", "b", "c"),
		rankRule("standard"),
		raceRule("standard", 2),
		rule("retry", "standard", func(r *RoutingRule) {
			r.Scope = "next"
			r.Count = 1
			r.Attempts = 1
			r.On = []string{"model_not_found"}
		}),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 4
			r.MaxInFlight = 3
			r.MaxCallsPerProvider = 1
		}),
	}, map[string][]string{"a": {"other"}, "b": {"other"}, "c": {"other"}})
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
	// Pre-validated failures must not consume the upstream call budget, so the
	// executor never runs for any target.
	if calls.Load() != 0 {
		t.Fatalf("catalog-rejected targets were dispatched upstream: %d calls", calls.Load())
	}
}

func TestModelNotFoundNeverSelectsUnrelatedModel(t *testing.T) {
	// A missing native must surface as model_not_found to the caller, never as
	// a lexicographic substitution to an arbitrary catalog id.
	compiled, catalog := compiledWithCatalogs(t, []RoutingRule{
		mapRule("standard", "deepseek-ai/DeepSeek-V4", "a"),
		rankRule("standard"),
		raceRule("standard", 1),
	}, map[string][]string{"a": {"alpha", "zeta"}})
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
	// Hyperfusion-style scenario: the provider appears in the primary stage
	// with one alias and in the fallback stage with a second alias. When the
	// primary alias is locally absent, the fallback reaches the same provider
	// once with its other native; the per-provider budget is not burned by the
	// pre-validated primary target.
	compiled, catalog := compiledWithCatalogs(t, []RoutingRule{
		mapRule("standard", "deepseek-ai/DeepSeek-V4", "a", "b"),
		rankRule("standard"),
		raceRule("standard", 2),
		rule("semaphore", "standard", func(r *RoutingRule) {
			r.MaxCalls = 4
			r.MaxInFlight = 3
			r.MaxCallsPerProvider = 1
		}),
		mapRule("standard", "gonka/deepseek-ai/DeepSeek-V4", "a"),
		rule("fallback", "standard", func(r *RoutingRule) {
			r.On = []string{"model_not_found", "5xx"}
			r.FallbackStrategy = "race"
		}),
	}, map[string][]string{
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
	// The primary arrays both fail model_not_found locally, so the fallback
	// dispatches provider a once with its second native. The catalog-rejected
	// primary never burned the per-provider call budget (maxCallsPerProvider=1).
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
