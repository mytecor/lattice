package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// repetitionConfig builds a two-provider streaming entry route for logical
// model "standard" that declares the continue policy (so the loop guard has a
// takeover path) and the repetition loop-guard policy on top of it. The
// continue rule carries a generous hold horizon so the takeover completes
// without entering the hold/keep-alive path.
func repetitionConfig(t *testing.T) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:2] // a, b
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		continueRuleWait("standard", 90*time.Second, "full", 0, 50*time.Millisecond),
		repetitionRule("standard"),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

// TestServerStopsRepetitionLoopAndContinues is the headline end-to-end
// contract of the loop guard: the winning provider (a) emits a self-
// repeating fragment ("Tool call.") with no finish_reason — the exact loop
// that ballooned the observed turn — and the gateway trips the guard on the
// K-th repeat, stops relaying the loop, and hands the same SSE stream off to
// the continuation (b). The client must see the previous non-loop content,
// the continuation's answer and [DONE], and never the repeating garbage or an
// error frame.
func TestServerStopsRepetitionLoopAndContinues(t *testing.T) {
	compiled := repetitionConfig(t)
	var aFirst int32 // whether the continuation provider has answered yet
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 8)
			switch target.Provider {
			case "a":
				// Winner: a short preface, then the unending "Tool call." loop.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"Let me handle this."}}]}`), Meaningful: true}
				for i := 0; i < 6; i++ {
					ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"Tool call."}}]}`), Meaningful: true}
				}
				close(ch)
				return ch, nil
			case "b":
				if atomic.AddInt32(&aFirst, 1) == 1 {
					// First call is the initial race: stay silent so a wins;
					// when a wins the branch is cancelled and this closes.
					go func() {
						<-ctx.Done()
						close(ch)
					}()
					return ch, nil
				}
				// Continuation: answer the actual question, then finish.
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"42"}}]}`), Meaningful: true}
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
				close(ch)
				return ch, nil
			}
			close(ch)
			return ch, nil
		},
	}
	api, runner := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)
	if !strings.Contains(body, "Let me handle this.") {
		t.Errorf("the pre-loop content must be relayed: %s", body)
	}
	// …the continuation answers on the same stream…
	if !strings.Contains(body, "42") {
		t.Errorf("the continuation must answer on the same stream: %s", body)
	}
	// …and the stream completes normally without an error frame.
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Errorf("a stopped loop must still end with [DONE]: %s", body)
	}
	if strings.Contains(body, "event: error") || strings.Contains(body, "upstream stream failed") {
		t.Errorf("a stopped loop must not surface a terminal error: %s", body)
	}
	// The guard must stop the repeating stream rather than relay all six
	// copies: the loop run that tripped the detector is cut short. (The first
	// K-1 fragments after the preface are legitimately relayed — the guard
	// only fires once K consecutive identical units accumulate.)
	if n := strings.Count(body, "Tool call."); n > 3 {
		t.Errorf("the repeating loop must be cut short (relayed %d loop fragments, want <= 3): %s", n, body)
	}
	// The loop-guard metric fires exactly once for this detection.
	mbody := scrape(t, runner.Metrics())
	if !strings.Contains(mbody, `llm_repetition_detected_total{route="standard",provider="a"} 1`) {
		t.Errorf("the loop-guard metric must record exactly one detection: %s", mbody)
	}
}

// TestServerRepetitionLoopTakeoverRejectsTheLoopStream pins that the guard
// stops a loop with no preface: as soon as the run reaches K identical units
// the relay halts, so the takeaway stream starts with the continuation rather
// than carrying the tail of the loop.
func TestServerRepetitionLoopTakeoverRejectsTheLoopStream(t *testing.T) {
	compiled := repetitionConfig(t)
	var bCalls int32
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 8)
			switch target.Provider {
			case "a":
				// Loop with no preface at all; the relay must stop once the run
				// reaches K identical units and hand off to the continuation. The
				// "REPEAT! " fragment carries a trailing delimiter so the detector
				// can see the repeated units (a delimiter-free run like
				// "repeatrepeat…" has no byte boundary to align on).
				for i := 0; i < 5; i++ {
					ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"REPEAT! "}}]}`), Meaningful: true}
				}
				close(ch)
				return ch, nil
			case "b":
				if atomic.AddInt32(&bCalls, 1) == 1 {
					go func() {
						<-ctx.Done()
						close(ch)
					}()
					return ch, nil
				}
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"answer"}}]}`), Meaningful: true}
				ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), Meaningful: false}
				close(ch)
				return ch, nil
			}
			close(ch)
			return ch, nil
		},
	}
	api, _ := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// The relay stops the run once K identical units accumulate: not all five
	// "REPEAT! " fragments make it through (K-1 = 3 legitimately do).
	if n := strings.Count(body, "REPEAT! "); n > 3 {
		t.Errorf("the loop must be cut short once the run trips (relayed %d REPEAT fragments, want <= 3): %s", n, body)
	}
	// The continuation answers after the loop is stopped, and the stream ends
	// with [DONE] — no error.
	if !strings.Contains(body, "answer") {
		t.Errorf("the continuation must answer after the loop is stopped: %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Errorf("must end with [DONE], got: %s", body)
	}
}

// TestServerRepetitionNotArmedByDefault pins the opt-in contract: without a
// repetition rule on the route, a repeating stream is relayed verbatim — no
// crash, no takeover, no truncation. The guard changes nothing unless the
// operator arms it (here there is not even a continue rule, so the relay has
// no loop-related machinery at all).
func TestServerRepetitionNotArmedByDefault(t *testing.T) {
	compiled := raceOnlyConfig(t) // plain race, no continue, no repetition
	var aCalls int
	executor := &fakeExecutor{
		stream: func(ctx context.Context, target Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
			ch := make(chan StreamEvent, 8)
			switch target.Provider {
			case "a":
				aCalls++
				for i := 0; i < 5; i++ {
					ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"Tool call."}}]}`), Meaningful: true}
				}
				close(ch)
				return ch, nil
			case "b":
				go func() {
					<-ctx.Done()
					close(ch)
				}()
				return ch, nil
			}
			close(ch)
			return ch, nil
		},
	}
	api, _ := streamTestServer(t, compiled, executor)

	body := postStream(t, api.URL)

	// Without a repetition rule the loop is relayed verbatim (the guard is
	// off); the relay does not invent a takeover for it.
	if !strings.Contains(body, "Tool call.") {
		t.Errorf("without a repetition rule the loop must pass through untouched: %s", body)
	}
	if aCalls != 1 {
		t.Errorf("without a repetition rule the winner must not be re-contacted (calls=%d)", aCalls)
	}
}

// TestServerRepetitionLoopSurfaceTerminalError pins that when the loop guard
// trips but no continuation is possible (the pool has no unused provider),
// the gateway surfaces the loop error through the normal stream error path —
// the client is told the relayed stream failed rather than left watching the
// loop. The failure is also fed back so the looping provider cools down.
func TestServerRepetitionLoopSurfaceTerminalError(t *testing.T) {
	compiled := repetitionSingleConfig(t)
	// The looping provider emits a stream with no preface; once the guard trips
	// (K repeats) there is no unused provider to continue onto.
	breakAll := func(ctx context.Context, _ Target, _ ExecuteRequest) (<-chan StreamEvent, *CallError) {
		ch := make(chan StreamEvent, 8)
		for i := 0; i < 5; i++ {
			ch <- StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"content":"Tool call."}}]}`), Meaningful: true}
		}
		close(ch)
		return ch, nil
	}
	api, _ := streamTestServer(t, compiled, &fakeExecutor{stream: breakAll})

	body := readStreamBody(t, api.URL, 5*time.Second)

	// The loop is cut short: K-1 fragments legitimately precede the trip, but
	// the rest are never relayed.
	if n := strings.Count(body, "Tool call."); n > 3 {
		t.Errorf("the loop must be cut short (relayed %d loop fragments, want <= 3): %s", n, body)
	}
	// With no continuation provider the terminal error surfaces (after the
	// bounded hold horizon) with the stable error text.
	if !strings.Contains(body, "event: error") {
		t.Errorf("an unterminated loop must eventually surface a terminal error: %s", body)
	}
	if !strings.Contains(body, "upstream stream failed") {
		t.Errorf("the loop error must carry the stable failure text: %s", body)
	}
}

// repetitionSingleConfig is a single-provider stream config that declares
// both the continue policy (a bounded hold horizon so the terminal error
// surfaces fast) and the repetition loop guard.
func repetitionSingleConfig(t *testing.T) *compiledConfig {
	t.Helper()
	cfg := testConfig()
	cfg.Providers = cfg.Providers[:1] // a only
	cfg.RoutingRules = []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
		continueRuleWait("standard", 90*time.Second, "full", 0, 50*time.Millisecond),
		repetitionRule("standard"),
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}
