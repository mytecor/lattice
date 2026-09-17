package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// scrape runs one exposition render and returns the body as text.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := m.WriteExposition(&buffer); err != nil {
		t.Fatalf("WriteExposition: %v", err)
	}
	return buffer.String()
}

func hashableFamily(body string) map[string]bool {
	lines := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		space := strings.IndexByte(line, ' ')
		if space < 0 {
			continue
		}
		lines[line[:space]] = true
	}
	return lines
}

func TestExpositionRendersFamilies(t *testing.T) {
	m := newMetrics()
	m.ObserveRequest("standard", "standard", "a", "success", 250*time.Millisecond)
	m.ObserveAttempt("a", "timeout")
	m.ObserveTTFT("standard", 120*time.Millisecond)
	m.ObserveTokens("standard", 100, 50)
	m.IncrInFlight("a")
	m.ObserveBalanceSelection("standard", "a")
	m.ObserveBalanceHealth("a", 0.8)
	m.ObserveCooldownUntil("a", time.Unix(1_700_000_000, 0))
	m.ObserveFallback("a", "b", "timeout")

	body := scrape(t, m)

	// Every promised family must be present with its TYPE line.
	for _, family := range []string{
		"llm_requests_total",
		"llm_attempts_total",
		"llm_fallbacks_total",
		"llm_balance_selections_total",
		"llm_input_tokens_total",
		"llm_output_tokens_total",
		"llm_requests_in_flight",
		"llm_balance_health",
		"llm_cooldown_until_seconds",
		"llm_request_duration_seconds",
		"llm_request_duration_seconds",
		"llm_request_duration_seconds",
		"llm_ttft_seconds",
		"llm_gateway_build_info",
	} {
		if !strings.Contains(body, "# TYPE "+family) {
			t.Errorf("exposition missing TYPE for %s", family)
		}
		if !strings.Contains(body, "# HELP "+family) {
			t.Errorf("exposition missing HELP for %s", family)
		}
	}
}

func TestExpositionNoHighCardinalityLabels(t *testing.T) {
	m := newMetrics()
	m.ObserveRequest("standard", "standard", "a", "success", time.Second)
	m.ObserveAttempt("a", "")
	// Simulate the worst case the registry will ever see: a label value is
	// drawn only from the fixed label dimensions.
	body := scrape(t, m)

	// High-cardinality identifiers must never appear as label names.
	for _, forbidden := range []string{
		"request_id", "session_id", "user_id", "api_key", "client_ip", "prompt",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("exposition leaks high-cardinality label %q", forbidden)
		}
	}
	// Label values are fixed-dimension enums, never values that grow with
	// traffic volume; nothing here should be a UUID or numeric id.
	if strings.Contains(body, "req_") {
		t.Errorf("exposition contains a request id as a label value")
	}
}

func TestHistogramBucketsAndSum(t *testing.T) {
	m := newMetrics()
	for _, value := range []float64{0.2, 0.4, 2.0, 10.0} {
		m.ObserveRequest("standard", "standard", "a", "success", time.Duration(value*float64(time.Second)))
	}
	body := scrape(t, m)

	// The cumulative semantics: a 2.0s observation lands in the 2.5 bucket but
	// not the 1.0 bucket.
	if !strings.Contains(body, `llm_request_duration_seconds_bucket{route="standard",model="standard",le="1"} 2`) {
		t.Errorf("expected cumulative histogram bucket le=1 to count 2, got:\n%s", body)
	}
	if !strings.Contains(body, `llm_request_duration_seconds_bucket{route="standard",model="standard",le="2.5"} 3`) {
		t.Errorf("expected cumulative histogram bucket le=2.5 to count 3, got:\n%s", body)
	}
	if !strings.Contains(body, "llm_request_duration_seconds_count{route=\"standard\",model=\"standard\"} 4") {
		t.Errorf("expected count 4")
	}
	if !strings.Contains(body, `le="+Inf"`) {
		t.Errorf("missing +Inf bucket")
	}
	// sum ≈ 0.2+0.4+2+10 = 12.6s
	if !strings.Contains(body, "llm_request_duration_seconds_sum{route=\"standard\",model=\"standard\"} 12.6") {
		t.Errorf("expected sum 12.6, got:\n%s", body)
	}
}

func TestInFlightAcrossConcurrentBranches(t *testing.T) {
	m := newMetrics()
	const workers = 16
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.IncrInFlight("a")
			m.ObserveAttempt("a", "")
			m.DecrInFlight("a")
		}()
	}
	wg.Wait()

	body := scrape(t, m)
	if !strings.Contains(body, `llm_requests_in_flight{provider="a"} 0`) {
		t.Errorf("in-flight gauge did not return to 0 after concurrent branches, got:\n%s", body)
	}
	// Attempts counter must be exact under concurrency.
	if !strings.Contains(body, `llm_attempts_total{provider="a",error_type=""} 16`) {
		t.Errorf("attempt counter not exact under concurrency, got:\n%s", body)
	}
}

func TestCounterSingleLinePerLabelSet(t *testing.T) {
	m := newMetrics()
	m.ObserveRequest("standard", "standard", "a", "success", time.Second)
	m.ObserveRequest("standard", "standard", "a", "success", time.Second)
	m.ObserveRequest("standard", "standard", "b", "success", time.Second)
	body := scrape(t, m)

	if !strings.Contains(body, `llm_requests_total{route="standard",model="standard",provider="a",status="success"} 2`) {
		t.Errorf("expected merged counter for provider a, got:\n%s", body)
	}
	if !strings.Contains(body, `llm_requests_total{route="standard",model="standard",provider="b",status="success"} 1`) {
		t.Errorf("expected counter for provider b, got:\n%s", body)
	}
}

func TestFallbacksMetric(t *testing.T) {
	m := newMetrics()
	m.ObserveFallback("a", "b", "timeout")
	m.ObserveFallback("a", "b", "5xx")
	body := scrape(t, m)
	if !strings.Contains(body, `llm_fallbacks_total{from_provider="a",to_provider="b",reason="timeout"} 1`) {
		t.Errorf("missing fallback timeout series")
	}
	if !strings.Contains(body, `llm_fallbacks_total{from_provider="a",to_provider="b",reason="5xx"} 1`) {
		t.Errorf("missing fallback 5xx series")
	}
}

func TestCooldownUntilMetric(t *testing.T) {
	m := newMetrics()
	m.ObserveCooldownUntil("a", time.Unix(1_700_000_100, 0))
	m.ObserveCooldownUntil("b", time.Unix(1_700_000_007, 0))
	m.ObserveCooldownUntil("c", time.Time{}) // clear: expired windows must not linger
	body := scrape(t, m)

	if !strings.Contains(body, `llm_cooldown_until_seconds{provider="a"} 1.7000001e+09`) {
		t.Errorf("cooldown deadline for a missing or wrong:\n%s", body)
	}
	if !strings.Contains(body, `llm_cooldown_until_seconds{provider="b"} 1.700000007e+09`) {
		t.Errorf("cooldown deadline for b missing or wrong:\n%s", body)
	}
	// A cleared provider must be fully absent from the family: a literal zero
	// deadline would be indistinguishable from a just-expired window.
	if strings.Contains(body, `llm_cooldown_until_seconds{provider="c"}`) {
		t.Errorf("cleared cooldown must not linger as a stale entry:\n%s", body)
	}
	// Extension overwrites in place: one series per provider, latest value wins.
	m.ObserveCooldownUntil("a", time.Unix(1_700_000_005, 0))
	body = scrape(t, m)
	if strings.Contains(body, `llm_cooldown_until_seconds{provider="a"} 1.7000001e+09`) {
		t.Errorf("stale cooldown deadline kept after extension:\n%s", body)
	}
}
