package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
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

func raceOnlyConfig(t *testing.T) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.RoutingRules = cfg.RoutingRules[:1]
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestRaceStartsAllProvidersAndFirstSuccessWins(t *testing.T) {
	compiled := raceOnlyConfig(t)
	catalog := newCatalog(compiled)
	started := make(chan string, 2)
	release := make(chan struct{})
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		<-release
		if target.Provider == "a" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		return []byte(`{"model":"native-model","winner":"b"}`), nil
	}}
	runner := newRunner(compiled, catalog, executor)
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
			t.Fatal("providers did not start concurrently")
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("not all providers started: %#v", seen)
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

func TestRaceCancelsLoser(t *testing.T) {
	compiled := raceOnlyConfig(t)
	started := make(chan string, 2)
	releaseWinner := make(chan struct{})
	loserCancelled := make(chan struct{})
	var once sync.Once
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		started <- target.Provider
		if target.Provider == "a" {
			<-releaseWinner
			return []byte(`{"winner":"a"}`), nil
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
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("streaming loser was not cancelled")
	}
}

func TestRetryWrapsPreviousRace(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = cfg.RoutingRules[:2]
	cfg.RoutingRules[1].Attempts = 1
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := map[string]int{}
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		mu.Lock()
		calls[target.Provider]++
		attempt := calls[target.Provider]
		mu.Unlock()
		if attempt == 1 {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		return []byte(`{"ok":true}`), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatalf("retry did not recover: %v", callErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"]+calls["b"] < 3 {
		t.Fatalf("race was not retried: %#v", calls)
	}
}

func TestFallbackRunsOnlyAfterMatchingFailure(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules[0].Providers = []string{"a"}
	cfg.RoutingRules = []RoutingRule{cfg.RoutingRules[0], cfg.RoutingRules[2]}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	executor := &fakeExecutor{do: func(_ context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		order = append(order, target.Provider)
		if target.Provider == "a" {
			return nil, &CallError{Class: ErrorUpstream, Status: 503}
		}
		return []byte(`{"winner":"b"}`), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	if _, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat}); callErr != nil {
		t.Fatalf("fallback failed: %v", callErr)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("unexpected fallback order: %#v", order)
	}
}

func TestHedgeDelaysSecondProvider(t *testing.T) {
	cfg := testConfig()
	cfg.RoutingRules = cfg.RoutingRules[:1]
	cfg.RoutingRules[0].Action = "hedge"
	cfg.RoutingRules[0].After = Duration{25 * time.Millisecond}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	firstStarted := make(chan time.Time, 1)
	secondStarted := make(chan time.Time, 1)
	executor := &fakeExecutor{do: func(ctx context.Context, target Target, _ ExecuteRequest) ([]byte, *CallError) {
		if target.Provider == "a" {
			firstStarted <- time.Now()
			<-ctx.Done()
			return nil, &CallError{Class: ErrorCancelled, Status: 499}
		}
		secondStarted <- time.Now()
		return []byte(`{"winner":"b"}`), nil
	}}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	result := make(chan *CallError, 1)
	go func() {
		_, callErr := runner.Run(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
		result <- callErr
	}()
	first := <-firstStarted
	second := <-secondStarted
	if second.Sub(first) < 20*time.Millisecond {
		t.Fatalf("hedge launched too early: %s", second.Sub(first))
	}
	if callErr := <-result; callErr != nil {
		t.Fatalf("hedge failed: %v", callErr)
	}
}

func TestStageTimeoutIsClassified(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules[0].Providers = []string{"a"}
	var timeout RoutingRule
	timeout.Match.Model = "standard"
	timeout.Action = "timeout"
	timeout.Duration = Duration{10 * time.Millisecond}
	cfg.RoutingRules = []RoutingRule{cfg.RoutingRules[0], timeout}
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
		t.Fatalf("timeout classification mismatch: %#v", callErr)
	}
}

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
		return []byte(`{"winner":"b"}`), nil
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

func TestSelectedStreamHonorsClientCancellation(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules[0].Providers = []string{"a"}
	cfg.RoutingRules = cfg.RoutingRules[:1]
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

func TestStreamingStageTimeoutStopsAfterWinnerSelection(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules[0].Providers = []string{"a"}
	var timeout RoutingRule
	timeout.Match.Model = "standard"
	timeout.Action = "timeout"
	timeout.Duration = Duration{50 * time.Millisecond}
	cfg.RoutingRules = []RoutingRule{cfg.RoutingRules[0], timeout}
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
				case <-time.After(75 * time.Millisecond):
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
			t.Fatalf("selected stream ended at the first-token deadline: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("selected stream did not finish")
	}
}

func TestStreamingStageTimeoutRetriesBeforeWinner(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1]
	cfg.RoutingRules[0].Providers = []string{"a"}
	var timeout RoutingRule
	timeout.Match.Model = "standard"
	timeout.Action = "timeout"
	timeout.Duration = Duration{20 * time.Millisecond}
	retry := cfg.RoutingRules[1]
	retry.Attempts = 1
	retry.On = []string{"timeout"}
	cfg.RoutingRules = []RoutingRule{cfg.RoutingRules[0], timeout, retry}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := 0
	var routeAttempts []int
	executor := &fakeExecutor{
		do: func(context.Context, Target, ExecuteRequest) ([]byte, *CallError) { return nil, nil },
		stream: func(ctx context.Context, _ Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			mu.Lock()
			calls++
			attempt := calls
			routeAttempt, _ := ctx.Value(routeAttemptKey).(int)
			routeAttempts = append(routeAttempts, routeAttempt)
			mu.Unlock()
			stream := make(chan StreamEvent, 1)
			if attempt == 1 {
				go func() {
					<-ctx.Done()
					close(stream)
				}()
				return stream, nil
			}
			stream <- StreamEvent{Data: []byte(`{"choices":[{"delta":{"content":"winner"}}]}`), Meaningful: true}
			close(stream)
			return stream, nil
		},
	}
	runner := newRunner(compiled, newCatalog(compiled), executor)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	selected, callErr := runner.SelectStream(context.Background(), "standard", ExecuteRequest{Kind: RequestChat})
	if callErr != nil {
		t.Fatalf("retry did not recover from first-token timeout: %v", callErr)
	}
	defer selected.Cancel()
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("expected one timed-out attempt and one retry, got %d calls", calls)
	}
	if len(routeAttempts) != 2 || routeAttempts[0] != 1 || routeAttempts[1] != 2 {
		t.Fatalf("routing attempt context was not preserved: %#v", routeAttempts)
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
