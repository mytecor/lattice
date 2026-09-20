package main

import (
	"bufio"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// version is the gateway build version, injected at package build time; it is
// exposed in a single llm_gateway_build_info gauge so a scrape can tell which
// binary produced the series. It is a variable (not a const) only so tests may
// pin it; production builds bake it in via -ldflags.
var version = "0.1.0"

// Metrics is the gateway's numeric observability surface. It is deliberately
// label-restricted (low cardinality only: route, model, provider, native_model,
// status and error_type are the only dimensions) and rendered on demand in the
// Prometheus text exposition format. request_id, session_id, api_key,
// client_ip or any prompt material never become labels. "model" is always the
// logical gateway model (1:1 with the of a request), "native_model" the
// provider's real model ID: both are bounded by the catalog (provider × native
// model, ~a dozen live pairs), so cardinality stays low.
//
// The registry is a hand-rolled minimal implementation on the standard
// library: the repository carries no vendor metrics dependency, and the
// surface this gateway needs (counters, histograms, gauges, a stable text
// renderer) is small and testable without dragging in a large dependency tree.
type Metrics struct {
	mu sync.RWMutex

	// requestsTotal counts completed client requests by route, model (logical),
	// winning provider, native model and final status.
	requestsTotal *counterVec
	// attemptTotal counts upstream branch attempts by provider, native model and
	// terminal error class (the empty error_type labels a successful attempt,
	// mirroring the event-level model).
	attemptTotal *counterVec
	// fallbackTotal counts explicit fallback transitions. The same transition
	// may fire multiple times per request; the counter reflects that.
	fallbackTotal *counterVec
	// continueTotal counts in-gateway stream continuations (the "continue"
	// rule), split by handoff kind: "takeover" is a single-provider handoff to
	// an unused provider, "chain_retry" is a full re-dispatch of an exhausted
	// chain from the top. Labels carry the source and destination provider.
	continueTotal *counterVec
	// chainRetriesTotal counts whole-chain retries (an exhausted continue
	// chain re-dispatched from the top), split by outcome: "started" on each
	// fresh pass, "completed" on the pass that finally won, "exhausted" when
	// the budget ran out without any pass winning. continueTotal above only
	// fires on a successful chain_retry handoff; this separate family makes
	// the retries that did not succeed observable too.
	chainRetriesTotal *counterVec
	// streamBreaks counts mid-stream (post-selection) winner stream failures
	// by provider, native model and error class. These failures escape the
	// scheduler (the route graph already returned a winner); the
	// stream-failure feedback increments llm_attempts_total too, and this
	// dedicated slice keeps mid-stream failures observable apart from
	// branch-scoped attempts.
	streamBreaks *counterVec
	// balanceSelections counts the provider chosen by the balance action per
	// route, so the p2c / round_robin / adaptive selection movement is
	// observable without reading scheduler internals.
	balanceSelections *counterVec
	// inputTokens / outputTokens accumulate usage from responses and streams,
	// keyed by logical model, provider and native model (zero when a provider
	// omits usage).
	inputTokens  *counterVec
	outputTokens *counterVec
	// requestDuration / ttft are cumulative histograms with stable buckets so
	// p50/p95 series share the same le edges across restarts.
	requestDuration *histogramVec
	ttft            *histogramVec
	// requestsInFlight is a provider-scoped gauge; it is bumped when a branch
	// starts and released when the branch completes or is cancelled.
	requestsInFlight *gaugeVec
	// balanceHealth is the last computed balance health per provider,
	// snapshotted by the runner alongside balance selections.
	balanceHealth *gaugeVec
	// cooldownUntil is the unix-second deadline until which a (provider,
	// native_model) pair is cooling, snapshotted by the runner whenever
	// cooldown state changes. A pair that is not cooling is absent from the
	// family (not zero): absence is the healthy state, and a literal zero
	// deadline would be indistinguishable from a just-expired (and thus no
	// longer cooling) window. Panels compute the remaining window at scrape
	// time with deadline − time() so the value decays truthfully between
	// snapshots.
	cooldownUntil *gaugeVec

	startTime time.Time
}

const (
	metricService = "llm-gateway"
)

// Bucket schedules for the request-duration and TTFT histograms. They are
// package-level, stable values so restarts share the same le edges.
var (
	// requestDurationBucketsSec spans both interactive streaming (hundreds of
	// ms) and long non-streaming (tens of seconds).
	requestDurationBucketsSec = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}
	// ttftBucketsSec ends at 10s because TTFT beyond that is effectively a dead
	// branch.
	ttftBucketsSec = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
)

func newMetrics() *Metrics {
	return &Metrics{
		requestsTotal:     newCounterVec([]string{"route", "model", "provider", "native_model", "status"}),
		attemptTotal:      newCounterVec([]string{"provider", "native_model", "error_type"}),
		streamBreaks:      newCounterVec([]string{"provider", "native_model", "error_type"}),
		fallbackTotal:     newCounterVec([]string{"from_provider", "to_provider", "reason"}),
		continueTotal:     newCounterVec([]string{"from_provider", "to_provider", "kind"}),
		chainRetriesTotal: newCounterVec([]string{"status"}),
		balanceSelections: newCounterVec([]string{"route", "provider"}),
		inputTokens:       newCounterVec([]string{"model", "provider", "native_model"}),
		outputTokens:      newCounterVec([]string{"model", "provider", "native_model"}),
		requestDuration:   newHistogramVec([]string{"route", "model", "provider", "native_model"}, requestDurationBucketsSec),
		ttft:              newHistogramVec([]string{"model", "provider", "native_model"}, ttftBucketsSec),
		requestsInFlight:  newGaugeVec([]string{"provider"}),
		balanceHealth:     newGaugeVec([]string{"provider"}),
		cooldownUntil:     newGaugeVec([]string{"provider", "native_model"}),
		startTime:         time.Now(),
	}
}

// ObserveRequest records one completed client request at the request level.
// The duration spans the whole request, the provider is the winner and status
// is "success" or "failed". model is the logical gateway model; native is the
// winner's provider model ID (empty when no branch ever succeeded, e.g. a
// pre-dispatch rejection).
func (m *Metrics) ObserveRequest(route, model, provider, native, status string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestsTotal.inc(route, model, provider, native, status)
	m.requestDuration.observe([]string{route, model, provider, native}, duration.Seconds())
}

// ObserveAttempt records one completed upstream branch. errorType is the
// terminal ErrorClass string and the empty string labels a success. native is
// the branch target's provider model ID.
func (m *Metrics) ObserveAttempt(provider, native, errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attemptTotal.inc(provider, native, errorType)
}

// ObserveTTFT records the winner's time-to-first-meaningful-event. provider
// is the winner's provider, native the winner's provider model ID.
func (m *Metrics) ObserveTTFT(model, provider, native string, duration time.Duration) {
	if duration <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ttft.observe([]string{model, provider, native}, duration.Seconds())
}

// ObserveTokens accumulates usage tokens for a model. Providers that omit
// usage contribute zero; the caller decides whether zero means "no tokens" or
// "unknown" (the structured events mark unknown usage explicitly, metrics do
// not carry an unknown carrier dimension). native is the provider model ID.
func (m *Metrics) ObserveTokens(model, provider, native string, input, output int64) {
	if input == 0 && output == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputTokens.add(input, model, provider, native)
	m.outputTokens.add(output, model, provider, native)
}

// IncrInFlight and DecrInFlight drive the in-flight gauge; they are kept
// separate from ObserveAttempt because a branch's in-flight flag tracks its
// own launch edge while the gauge reflects current concurrency.
func (m *Metrics) IncrInFlight(provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestsInFlight.inc(provider)
}

func (m *Metrics) DecrInFlight(provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestsInFlight.dec(provider)
}

// ObserveStreamBreak records one mid-stream (post-selection) winner stream
// failure. errorType is the terminal ErrorClass string, native the winner's
// provider model ID.
func (m *Metrics) ObserveStreamBreak(provider, native, errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamBreaks.inc(provider, native, errorType)
}

// ObserveFallback records an explicit fallback transition.
func (m *Metrics) ObserveFallback(fromProvider, toProvider, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallbackTotal.inc(fromProvider, toProvider, reason)
}

// ObserveContinue records an in-gateway stream continuation (the "continue"
// rule). kind is "takeover" for a single-provider handoff to an unused
// provider, or "chain_retry" for a full re-dispatch of an exhausted chain
// from the top.
func (m *Metrics) ObserveContinue(fromProvider, toProvider, kind string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.continueTotal.inc(fromProvider, toProvider, kind)
}

// ObserveChainRetry records a whole-chain retry (an exhausted continue chain
// re-dispatched from the top). status is "started" for each fresh pass,
// "completed" for the pass that finally produced a winner, or "exhausted"
// when the budget ran out without any pass winning.
func (m *Metrics) ObserveChainRetry(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chainRetriesTotal.inc(status)
}

// ObserveBalanceSelection and ObserveBalanceHealth record the balance action:
// which provider was chosen for a route and the current health of each
// provider in the pool.
func (m *Metrics) ObserveBalanceSelection(route, provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balanceSelections.inc(route, provider)
}

func (m *Metrics) ObserveBalanceHealth(provider string, health float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balanceHealth.set(provider, health)
}

// ObserveCooldownUntil records the unix-second deadline until which a
// (provider, native_model) pair is cooling. A zero deadline clears the pair
// from the family: a cleared (expired or success-reset) cooldown must not
// linger as a stale entry, so the dashboard's presence-based panels stay
// truthful.
func (m *Metrics) ObserveCooldownUntil(provider, native string, until time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until.IsZero() {
		m.cooldownUntil.clear(provider, native)
		return
	}
	m.cooldownUntil.set(provider, native, float64(until.UnixNano())/1e9)
}

// WriteExposition renders the full metric surface in the Prometheus text
// exposition format (version 0.0.4: TYPE/HELP lines plus one line per unique
// label set, families sorted by name). It also emits a small set of
// runtime/process gauges so a scrape carries basic node health without an
// external agent. Rendering is read-only on the registry after the READ lock,
// so concurrent scrapes serialize harmlessly.
func (m *Metrics) WriteExposition(writer io.Writer) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	buffered := bufio.NewWriter(writer)
	emit := func(line string) {
		_, _ = buffered.WriteString(line)
		_ = buffered.WriteByte('\n')
	}

	emit(fmt.Sprintf("# HELP llm_gateway_build_info Build information for the Lattice LLM gateway."))
	emit(fmt.Sprintf("# TYPE llm_gateway_build_info gauge"))
	emit(fmt.Sprintf("llm_gateway_build_info{version=%q,service=%q} 1", version, metricService))
	emitRuntimeMetrics(buffered, m.startTime)

	m.requestsTotal.write(buffered, "llm_requests_total", "Completed client requests by route, logical model, winning provider, native model and status.")
	m.attemptTotal.write(buffered, "llm_attempts_total", "Upstream branch attempts by provider, native model and terminal error type.")
	m.streamBreaks.write(buffered, "llm_stream_breaks_total", "Mid-stream (post-selection) winner stream failures by provider, native model and error type.")
	m.fallbackTotal.write(buffered, "llm_fallbacks_total", "Explicit fallback transitions by source and destination provider.")
	m.continueTotal.write(buffered, "llm_continues_total", "In-gateway stream continuations (continue rule) by source/destination provider and kind (takeover vs chain_retry).")
	m.chainRetriesTotal.write(buffered, "llm_chain_retries_total", "Whole-chain retries of an exhausted continue chain by outcome (started, completed, exhausted).")
	m.balanceSelections.write(buffered, "llm_balance_selections_total", "Provider chosen by the balance action per route.")
	m.inputTokens.write(buffered, "llm_input_tokens_total", "Accumulated input tokens by logical model, provider and native model.")
	m.outputTokens.write(buffered, "llm_output_tokens_total", "Accumulated output tokens by logical model, provider and native model.")
	m.requestsInFlight.write(buffered, "llm_requests_in_flight", "Current in-flight upstream branches by provider.")
	m.balanceHealth.write(buffered, "llm_balance_health", "Latest balance health score per provider (0 unhealthy … 1 healthy).")
	m.cooldownUntil.write(buffered, "llm_cooldown_until_seconds", "Unix seconds until a cooling provider native-model pair re-enters the candidate pool.")
	m.requestDuration.write(buffered, "llm_request_duration_seconds", "Request duration histogram by route, logical model, provider and native model.")
	m.ttft.write(buffered, "llm_ttft_seconds", "Time to first meaningful event histogram by logical model, provider and native model.")

	return buffered.Flush()
}

// emitRuntimeMetrics writes the small Go runtime / process surface. Names are
// chosen to avoid colliding with real node_* exporter names yet stay usable in
// a PromQL dashboard.
func emitRuntimeMetrics(buffered *bufio.Writer, startTime time.Time) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	buffered.WriteString("# HELP go_goroutines Number of goroutines.\n")
	buffered.WriteString("# TYPE go_goroutines gauge\n")
	fmt.Fprintf(buffered, "go_goroutines %d\n", runtime.NumGoroutine())

	buffered.WriteString("# HELP go_memstats_alloc_bytes Number of heap bytes allocated and still in use.\n")
	buffered.WriteString("# TYPE go_memstats_alloc_bytes gauge\n")
	fmt.Fprintf(buffered, "go_memstats_alloc_bytes %d\n", mem.Alloc)

	buffered.WriteString("# HELP go_memstats_heap_objects Number of heap objects allocated and still in use.\n")
	buffered.WriteString("# TYPE go_memstats_heap_objects gauge\n")
	fmt.Fprintf(buffered, "go_memstats_heap_objects %d\n", mem.HeapObjects)

	buffered.WriteString("# HELP process_start_time_seconds Start time of the process since unix epoch in seconds.\n")
	buffered.WriteString("# TYPE process_start_time_seconds gauge\n")
	fmt.Fprintf(buffered, "process_start_time_seconds %g\n", float64(startTime.UnixNano())/1e9)
}

// ---------------------------------------------------------------------------
// labelSet is the ordered label/value tuple of one metric line.
// ---------------------------------------------------------------------------

type labelSet struct {
	names  []string
	values []string
}

func (ls labelSet) key() string {
	return strings.Join(ls.values, "\x00")
}

func (ls labelSet) render() string {
	return "{" + ls.inner() + "}"
}

// inner renders the comma-separated name="value" pairs without the wrapping
// braces, used where an extra label (a histogram le edge) joins the set.
func (ls labelSet) inner() string {
	if len(ls.values) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, name := range ls.names {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(name)
		builder.WriteByte('=')
		builder.WriteString(strconv.Quote(ls.values[i]))
	}
	return builder.String()
}

// ---------------------------------------------------------------------------
// counterVec: a family of counters indexed by an ordered label set.
// ---------------------------------------------------------------------------

type counterVec struct {
	ints       map[string]int64
	labelNames []string
}

func newCounterVec(labelNames []string) *counterVec {
	return &counterVec{ints: make(map[string]int64), labelNames: labelNames}
}

func (c *counterVec) keyFor(values ...string) string {
	return strings.Join(values, "\x00")
}

func (c *counterVec) inc(values ...string) {
	c.add(1, values...)
}

func (c *counterVec) add(delta int64, values ...string) {
	key := c.keyFor(values...)
	c.ints[key] += delta
}

func (c *counterVec) write(buffered *bufio.Writer, name, help string) {
	if len(c.ints) == 0 {
		return
	}
	keys := make([]string, 0, len(c.ints))
	for key := range c.ints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buffered.WriteString("# HELP " + name + " " + help + "\n")
	buffered.WriteString("# TYPE " + name + " counter\n")
	for _, key := range keys {
		values := strings.Split(key, "\x00")
		set := labelSet{names: c.labelNames, values: values}
		fmt.Fprintf(buffered, "%s%s %d\n", name, set.render(), c.ints[key])
	}
}

// ---------------------------------------------------------------------------
// gaugeVec: same shape as counterVec but values are sets (replace).
// ---------------------------------------------------------------------------

type gaugeVec struct {
	v          map[string]float64
	labelNames []string
}

func newGaugeVec(labelNames []string) *gaugeVec {
	return &gaugeVec{v: make(map[string]float64), labelNames: labelNames}
}

func (g *gaugeVec) keyFor(values ...string) string {
	return strings.Join(values, "\x00")
}

func (g *gaugeVec) inc(values ...string) {
	g.v[g.keyFor(values...)]++
}

func (g *gaugeVec) dec(values ...string) {
	value := g.v[g.keyFor(values...)]
	value--
	if value < 0 {
		value = 0
	}
	g.v[g.keyFor(values...)] = value
}

// set writes the last value under the given label values; the final argument
// is the gauge value, the rest are label values.
func (g *gaugeVec) set(values ...any) {
	key := make([]string, 0, len(values)-1)
	for _, value := range values[:len(values)-1] {
		key = append(key, value.(string))
	}
	g.v[g.keyFor(key...)] = values[len(values)-1].(float64)
}

// clear removes one label set's entry entirely. Cooldown state uses this so an
// expired window disappears from the exposition instead of lingering as a
// stale 0: the value may only be observed on the next scrape after expiry,
// and by then absence is the truthful value.
func (g *gaugeVec) clear(provider, model string) {
	delete(g.v, g.keyFor(provider, model))
}

func (g *gaugeVec) write(buffered *bufio.Writer, name, help string) {
	if len(g.v) == 0 {
		return
	}
	keys := make([]string, 0, len(g.v))
	for key := range g.v {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buffered.WriteString("# HELP " + name + " " + help + "\n")
	buffered.WriteString("# TYPE " + name + " gauge\n")
	for _, key := range keys {
		values := strings.Split(key, "\x00")
		set := labelSet{names: g.labelNames, values: values}
		fmt.Fprintf(buffered, "%s%s %g\n", name, set.render(), g.v[key])
	}
}

// ---------------------------------------------------------------------------
// histogramVec: a family of cumulative histograms.
// ---------------------------------------------------------------------------

type histogramVec struct {
	v          map[string]*histogram
	labelNames []string
	buckets    []float64
}

func newHistogramVec(labelNames []string, buckets []float64) *histogramVec {
	return &histogramVec{
		v:          make(map[string]*histogram),
		labelNames: labelNames,
		buckets:    append([]float64(nil), buckets...),
	}
}

// histogram is one cumulative bucket accumulator. count and sum follow the
// Prometheus semantics: sum is the sum of observed values, count the total
// observations, and each bucket the cumulative count of observations ≤ le.
type histogram struct {
	le           []float64
	counts       []int64
	observations int64
	sum          float64
}

func (h *histogram) observe(value float64) {
	h.observations++
	h.sum += value
	for i, le := range h.le {
		if value <= le {
			h.counts[i]++
		}
	}
}

// write renders one histogram family line set. label is the full label-set
// text already rendered by the caller, with no trailing punctuation; le edges
// are appended as additional labels for bucket lines.
func (h *histogram) write(buffered *bufio.Writer, name, label string) {
	for i, le := range h.le {
		fmt.Fprintf(buffered, "%s_bucket{%s,le=%q} %d\n", name, label, formatFloat(le), h.counts[i])
	}
	fmt.Fprintf(buffered, "%s_sum{%s} %g\n", name, label, h.sum)
	fmt.Fprintf(buffered, "%s_count{%s} %d\n", name, label, h.observations)
	fmt.Fprintf(buffered, "%s_bucket{%s,le=\"+Inf\"} %d\n", name, label, h.observations)
}

func (h *histogramVec) observe(values []string, value float64) {
	key := strings.Join(values, "\x00")
	hist := h.v[key]
	if hist == nil {
		hist = &histogram{le: append([]float64(nil), h.buckets...), counts: make([]int64, len(h.buckets))}
		h.v[key] = hist
	}
	hist.observe(value)
}

func (h *histogramVec) write(buffered *bufio.Writer, name, help string) {
	if len(h.v) == 0 {
		return
	}
	sets := make([]labelSet, 0, len(h.v))
	bySet := make(map[string]*histogram, len(h.v))
	for key, hist := range h.v {
		values := strings.Split(key, "\x00")
		set := labelSet{names: h.labelNames, values: values}
		sets = append(sets, set)
		bySet[set.key()] = hist
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].key() < sets[j].key() })
	buffered.WriteString("# HELP " + name + " " + help + "\n")
	buffered.WriteString("# TYPE " + name + " histogram\n")
	for _, set := range sets {
		bySet[set.key()].write(buffered, name, set.inner())
	}
}

// formatFloat renders a bucket edge in a stable, non-scientific form suitable
// for Prometheus label values.
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
