package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const maxRequestBody = 16 << 20

type Server struct {
	config          *compiledConfig
	catalog         *Catalog
	runner          *Runner
	mux             *http.ServeMux
	logger          *slog.Logger
	metrics         *Metrics
	requestSequence atomic.Uint64
}

func newServer(config *compiledConfig, catalog *Catalog, runner *Runner) *Server {
	server := &Server{
		config: config, catalog: catalog, runner: runner, mux: http.NewServeMux(), logger: config.logger,
		metrics: runner.Metrics(),
	}
	server.mux.HandleFunc("GET /healthz", server.health)
	server.mux.HandleFunc("GET /metrics", server.metricsHandler)
	server.mux.HandleFunc("GET /v1/models", server.auth(server.models))
	server.mux.HandleFunc("POST /v1/chat/completions", server.auth(server.chat))
	server.mux.HandleFunc("POST /v1/responses", server.auth(server.responses))
	server.mux.HandleFunc("POST /admin/models/refresh", server.auth(server.refresh))
	return server
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.mux.ServeHTTP(writer, request)
}

// newMetricsHandler builds a dedicated http.Handler serving only GET /metrics
// from the given registry. It is mounted on the separate loopback metrics
// listener and exposes no other endpoint, so a scrape cannot reach the client
// API surface.
func newMetricsHandler(metrics *Metrics) http.Handler {
	mux := http.NewServeMux()
	handler := &metricsOnly{metrics: metrics}
	mux.HandleFunc("GET /metrics", handler.serve)
	return mux
}

type metricsOnly struct {
	metrics *Metrics
}

func (m *metricsOnly) serve(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if err := m.metrics.WriteExposition(writer); err != nil {
		_, _ = io.WriteString(writer, "# error rendering metrics\n")
	}
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if s.config.raw.ClientAPIKey == "" {
			next(writer, request)
			return
		}
		provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(s.config.raw.ClientAPIKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.config.raw.ClientAPIKey)) != 1 {
			writeAPIError(writer, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		next(writer, request)
	}
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ok"})
}

// routeOf resolves the entry route name for a logical model, used as the
// route label on request-level metrics. Every discoverable logical model maps
// to exactly one entry route, so the label set stays low-cardinality.
func (s *Server) routeOf(logical string) string {
	if entry, ok := s.config.models[logical]; ok {
		return entry.Name
	}
	return ""
}

// metricsHandler handles GET /metrics without client authentication: the
// endpoint is deliberately non-public (its own loopback listener, see config)
// and must not require the client API key.
func (s *Server) metricsHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if err := s.metrics.WriteExposition(writer); err != nil {
		logEvent(context.Background(), s.logger, slog.LevelError, "metrics_exposition_failed", "detail", safeLogDetail(err.Error()))
	}
}

// observeRequest records the request-level observation: counter, histogram and
// token accumulation share one call so the request lifecycle is recorded once
// at exactly one terminal point. The empty provider conveys that no branch
// ever succeeded (for example a pre-dispatch rejection).
func (s *Server) observeRequest(logical, provider, status string, duration time.Duration) {
	s.metrics.ObserveRequest(s.routeOf(logical), logical, provider, status, duration)
}

// observeTokens accumulates usage from a non-stream response body and returns
// the parsed usage so the caller can embed it into the request_completed event.
// Providers that omit usage contribute nothing. The hasUsage flag tells whether
// the provider reported usage at all (zero tokens with hasUsage=false mean
// "unknown", not "zero").
func (s *Server) observeTokens(logical string, response []byte) (input, output, cached int64, hasUsage bool) {
	summary, ok := extractUsageFull(response)
	if !ok {
		return 0, 0, 0, false
	}
	s.metrics.ObserveTokens(logical, summary.Input, summary.Output)
	return summary.Input, summary.Output, summary.CachedTokens, true
}

func (s *Server) models(writer http.ResponseWriter, _ *http.Request) {
	created := time.Now().Unix()
	data := make([]map[string]any, 0, len(s.config.logicalIDs))
	for _, id := range s.config.logicalIDs {
		data = append(data, map[string]any{
			"id": id, "object": "model", "created": created, "owned_by": "lattice",
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) chat(writer http.ResponseWriter, request *http.Request) {
	s.inference(writer, request, RequestChat)
}

func (s *Server) responses(writer http.ResponseWriter, request *http.Request) {
	s.inference(writer, request, RequestResponses)
}

func (s *Server) inference(writer http.ResponseWriter, request *http.Request, kind RequestKind) {
	started := time.Now()
	requestID := strconv.FormatUint(s.requestSequence.Add(1), 10)
	request = request.WithContext(withRequestID(request.Context(), requestID))
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	if err != nil {
		logEvent(request.Context(), s.logger, slog.LevelWarn, "request_rejected",
			"kind", kind,
			"status_code", http.StatusBadRequest,
			"reason", "invalid_body",
		)
		writeAPIError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var metadata struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil || strings.TrimSpace(metadata.Model) == "" {
		logEvent(request.Context(), s.logger, slog.LevelWarn, "request_rejected",
			"kind", kind,
			"status_code", http.StatusBadRequest,
			"reason", "invalid_json_or_model",
		)
		writeAPIError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, exists := s.config.models[metadata.Model]; !exists {
		logEvent(request.Context(), s.logger, slog.LevelWarn, "request_rejected",
			"kind", kind,
			"logical_model", metadata.Model,
			"status_code", http.StatusNotFound,
			"reason", "model_not_found",
		)
		writeAPIError(writer, http.StatusNotFound, "model_not_found")
		return
	}
	logEvent(request.Context(), s.logger, slog.LevelInfo, "request_received",
		"kind", kind,
		"logical_model", metadata.Model,
		"stream", metadata.Stream,
	)
	executeRequest := ExecuteRequest{Kind: kind, Body: body}
	if metadata.Stream {
		s.stream(writer, request, metadata.Model, executeRequest, started)
		return
	}
	result, callErr := s.runner.RunWithResult(request.Context(), metadata.Model, executeRequest)
	if callErr != nil {
		logEvent(request.Context(), s.logger, slog.LevelWarn, "request_failed",
			"kind", kind,
			"logical_model", metadata.Model,
			"stream", metadata.Stream,
			"status_code", callErrorStatus(callErr),
			"error_type", string(callErr.Class),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		s.observeRequest(metadata.Model, "", "failed", time.Since(started))
		writeCallError(writer, callErr)
		return
	}
	response := result.Body
	// Bind the winning provider to the opaque Responses state identifiers;
	// the identifiers themselves and the request body never reach the logs.
	if kind == RequestResponses {
		s.bindResponsesAffinity(metadata.Model, result.Provider, response, "")
	}
	provider := result.Provider
	input, output, cached, hasUsage := s.observeTokens(metadata.Model, response)
	response, err = sanitizeJSON(response, metadata.Model)
	if err != nil {
		logEvent(request.Context(), s.logger, slog.LevelError, "request_failed",
			"kind", kind,
			"logical_model", metadata.Model,
			"stream", metadata.Stream,
			"status_code", http.StatusBadGateway,
			"error_type", string(ErrorInvalid),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		s.observeRequest(metadata.Model, provider, "failed", time.Since(started))
		writeAPIError(writer, http.StatusBadGateway, "invalid_upstream_response")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(response)
	logEvent(request.Context(), s.logger, slog.LevelInfo, "request_completed",
		"kind", kind,
		"route", s.routeOf(metadata.Model),
		"logical_model", metadata.Model,
		"provider", provider,
		"stream", metadata.Stream,
		"status_code", http.StatusOK,
		"status", "success",
		"duration_ms", time.Since(started).Milliseconds(),
		"ttft_ms", result.TTFT.Milliseconds(),
		"input_tokens", input,
		"output_tokens", output,
		"cached_tokens", cached,
		"has_usage", hasUsage,
		"attempts", result.Attempts,
	)
	s.observeRequest(metadata.Model, provider, "success", time.Since(started))
}

func (s *Server) stream(writer http.ResponseWriter, request *http.Request, logical string, executeRequest ExecuteRequest, started time.Time) {
	selected, callErr := s.runner.SelectStream(request.Context(), logical, executeRequest)
	if callErr != nil {
		logEvent(request.Context(), s.logger, slog.LevelWarn, "request_failed",
			"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
			"status_code", callErrorStatus(callErr), "error_type", string(callErr.Class),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeCallError(writer, callErr)
		return
	}
	cancelCurrent := func() { selected.Cancel() }
	defer func() { cancelCurrent() }()
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeAPIError(writer, http.StatusInternalServerError, "streaming_not_supported")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)

	// Bind the winning provider to the opaque response id as it appears in the
	// winner's stream (response.created / response.completed events).
	bind := func(event StreamEvent) {
		if executeRequest.Kind != RequestResponses {
			return
		}
		s.bindResponsesAffinity(logical, selected.Provider, event.Data, event.Event)
	}
	// Usage is accumulated across the winner's buffered prelude and the
	// remaining stream (final usage chunk or delta fields); events report the
	// summed totals plus whether any usage was reported at all.
	var inTokens, outTokens, cachedTokens int64
	var hasUsage bool
	// A chat-completion winner stream must end with a choices[].finish_reason
	// chunk. A stream that closes cleanly without one is a truncated or empty
	// upstream response (observed 2026-09-17: gonka carriers cutting long GLM
	// streams at the ~300s mark and answering load with an instant empty
	// stream), not a completed request — relaying it as success ([DONE], no
	// error) makes the client finish an unfinished turn with
	// "Stream ended without finish_reason" while the provider keeps a clean
	// health record and keeps winning races. Such a close is therefore
	// surfaced as a retryable upstream failure fed into cooldown/health.
	sawFinishReason := false
	markFinish := func(data []byte) {
		if !sawFinishReason && executeRequest.Kind == RequestChat && extractFinishReason(data) {
			sawFinishReason = true
		}
	}
	requestID := requestIDFrom(request.Context())
	// The idle watchdog bounds a silently stalled winner stream: the route
	// deadline only guards the selection phase, so without it a winner that
	// stops emitting events hangs until the client gives up. Any event (not
	// only meaningful content — provider keep-alives count) re-arms the timer;
	// zero disables the watchdog. The timeout is deliberately generous so
	// legitimate reasoning pauses survive. When the route carries the
	// "continue" policy, its own idle threshold replaces the global one: the
	// stall is then a takeover trigger rather than a terminal error.
	continueCfg, continueEnabled := s.runner.continuePolicy(logical)
	idleTimeout := s.config.raw.StreamIdleTimeout.Duration
	if continueEnabled && continueCfg.Idle > 0 {
		idleTimeout = continueCfg.Idle
	}
	var idleTimer *time.Timer
	var idleC <-chan time.Time
	if idleTimeout > 0 {
		idleTimer = time.NewTimer(idleTimeout)
		defer idleTimer.Stop()
		idleC = idleTimer.C
	}
	rearmIdle := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(idleTimeout)
	}
	observe := func(data []byte) {
		if summary, ok := extractUsageFull(data); ok {
			inTokens += summary.Input
			outTokens += summary.Output
			cachedTokens += summary.CachedTokens
			hasUsage = true
			s.metrics.ObserveTokens(logical, summary.Input, summary.Output)
		}
	}
	// The partial output accumulator feeds the "continue" rule: every relayed
	// assistant delta (content and reasoning) is remembered so a takeover can
	// reshape it into assistant context for the next provider.
	partial := &partialStreamOutput{}
	observePartial := func(data []byte) { accumulatePartial(data, partial) }
	cur := selected
	// The providers whose streams have broken during this request. Only genuine
	// breaks accumulate here — providers that merely lost the original race
	// stay eligible for a takeover, so the continuation can still reach them.
	var brokenProviders []string
	// takeover attempts an in-gateway stream continuation: the current winner
	// stalled or broke, so the request is re-dispatched to a different
	// provider with the partial output reshared, and the same client SSE
	// stream is continued with the new winner. Returns true when a
	// continuation started; false leaves the caller to surface the terminal
	// error for `broken`. Only streaming chat requests participate.
	// swapTo adopts a freshly selected continuation stream as the current
	// winner on the same client SSE stream: cancels the previous winner, records
	// it in the broken set (so an in-chain continuation never re-hits it) and
	// relays the new winner's buffered prelude. Shared by single-provider
	// takeovers and whole-chain retries.
	swapTo := func(next *SelectedStream) bool {
		cur.Cancel()
		brokenProviders = append(brokenProviders, cur.Provider)
		cancelCurrent = next.Cancel
		cur = next
		sawFinishReason = false
		rearmIdle()
		for _, event := range cur.Buffered {
			observePartial(event.Data)
			observe(event.Data)
			markFinish(event.Data)
			if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
				return false
			}
		}
		return true
	}
	logoutContinue := func(event string, from string, next *SelectedStream) {
		logEvent(request.Context(), s.logger, slog.LevelInfo, event,
			"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
			"from_provider", from,
			"to_provider", next.Provider,
			"reshare", continueCfg.Reshare,
			"established_ms", time.Since(started).Milliseconds(),
		)
	}
	// dispatchContinue re-dispatches the request through the continue path with
	// the given broken-exclusion set, adopting the fresh winner. A nil result
	// reports that the re-dispatch produced no winner (ContinueStream error).
	dispatchContinue := func(broken []string) *SelectedStream {
		next, callErr := s.runner.ContinueStream(request.Context(), logical, executeRequest, partial, broken)
		if callErr != nil {
			logEvent(request.Context(), s.logger, slog.LevelWarn, "llm_continue_failed",
				"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
				"from_provider", cur.Provider,
				"status_code", callErrorStatus(callErr), "error_type", string(callErr.Class),
				"attempts", cur.Attempts,
			)
			return nil
		}
		return next
	}
	// chainRetriesLeft is the whole-chain retry budget: after a takeover has no
	// eligible provider left (every provider already broke during this request),
	// the gateway re-dispatches the entire chain from the top with the partial
	// output reshared, instead of surfacing a terminal error. Cooldown still
	// gates the just-broken providers, so the fresh pass races the healthy ones.
	chainRetriesLeft := continueCfg.Retries
	// takeover attempts an in-gateway stream continuation: the current winner
	// stalled or broke, so the request is re-dispatched to a different provider
	// with the partial output reshared, and the same client SSE stream is
	// continued with the new winner. When no provider remains (ContinueStream
	// finds no eligible target), the whole chain is re-dispatched from the top
	// while the chain-retry budget lasts. Returns true when any continuation
	// started; false leaves the caller to surface the terminal error.
	takeover := func(broken *CallError) bool {
		if !continueEnabled || executeRequest.Kind != RequestChat {
			return false
		}
		// Exclude the provider whose stream just broke from the immediate
		// takeover. brokenProviders only absorbs a broken provider when swapTo
		// appends it AFTER the dispatch that produced the next winner, so
		// without this the just-broke provider stays eligible for exactly one
		// more round: when it is also the fastest (or fail-open re-races the
		// pool while everyone is cooling), the continuation re-hits the same
		// upstream it should be fleeing — burning a full re-dispatch on the
		// provider the takeover exists to avoid ("continue on other
		// providers"). The set is deduplicated later by runtime.exclude, so the
		// overlap with swapTo's own append is harmless.
		excluded := append(append([]string(nil), brokenProviders...), cur.Provider)
		if next := dispatchContinue(excluded); next != nil {
			from := cur.Provider
			continued := swapTo(next)
			s.metrics.ObserveContinue(from, next.Provider, "takeover")
			logoutContinue("llm_continue", from, next)
			return continued
		}
		// The chain is exhausted: every provider in the pool broke during this
		// request. Re-dispatch the whole chain from the top, forgetting which
		// providers broke (cooldown still gates them), reusing the accumulated
		// partial output so the model continues instead of restarting. Only when
		// the budget is positive do we attempt whole-chain retries, so the
		// exhausted metric stays honest: zero retries means no whole-chain
		// re-dispatch happened at all, and that is just the plain terminal error.
		retried := false
		for chainRetriesLeft > 0 {
			chainRetriesLeft--
			retried = true
			logEvent(request.Context(), s.logger, slog.LevelWarn, "llm_chain_retry",
				"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
				"from_provider", cur.Provider,
				"retries_left", chainRetriesLeft,
				"established_ms", time.Since(started).Milliseconds(),
			)
			s.metrics.ObserveChainRetry("started")
			brokenProviders = nil
			if next := dispatchContinue(nil); next != nil {
				from := cur.Provider
				continued := swapTo(next)
				s.metrics.ObserveContinue(from, next.Provider, "chain_retry")
				s.metrics.ObserveChainRetry("completed")
				logoutContinue("llm_continue", from, next)
				return continued
			}
		}
		if retried {
			s.metrics.ObserveChainRetry("exhausted")
		}
		return false
	}
	for _, event := range selected.Buffered {
		bind(event)
		observePartial(event.Data)
		observe(event.Data)
		markFinish(event.Data)
		if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
			return
		}
	}
	for {
		select {
		case <-request.Context().Done():
			logEvent(request.Context(), s.logger, slog.LevelDebug, "request_cancelled",
				"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
				"provider", cur.Provider,
				"attempts", cur.Attempts,
				"duration_ms", time.Since(started).Milliseconds(),
			)
			s.observeRequest(logical, cur.Provider, "cancelled", time.Since(started))
			return
		case <-idleC:
			stall := &CallError{Class: ErrorTimeout, Status: 504, Cause: fmt.Errorf("no stream events for %s", idleTimeout)}
			logEvent(request.Context(), s.logger, slog.LevelWarn, "llm_stream_stalled",
				"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
				"provider", cur.Provider,
				"idle_ms", idleTimeout.Milliseconds(),
				"attempts", cur.Attempts,
				"duration_ms", time.Since(started).Milliseconds(),
			)
			s.runner.RecordStreamFailure(request.Context(), logical, cur.Provider, cur.Model, stall)
			if !sawFinishReason && takeover(stall) {
				continue
			}
			s.observeRequest(logical, cur.Provider, "failed", time.Since(started))
			writeStreamError(writer, flusher, stall, requestID, s.runner.streamRetryable(logical, stall))
			return
		case event, open := <-cur.Remaining:
			if !open {
				// A chat stream that closes without any finish_reason chunk did
				// not complete, whatever the transport says: the client would
				// end its turn on a truncated response. With the "continue" rule
				// the close is a takeover trigger; without it, surface the same
				// structured retryable error as a mid-stream break so the
				// provider enters cooldown and a retry-capable client retries.
				if executeRequest.Kind == RequestChat && !sawFinishReason {
					broken := &CallError{Class: ErrorUpstream, Status: http.StatusBadGateway,
						Cause: errors.New("winner stream ended without finish_reason")}
					logEvent(request.Context(), s.logger, slog.LevelWarn, "request_failed",
						"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
						"provider", cur.Provider,
						"status_code", callErrorStatus(broken), "error_type", string(broken.Class),
						"reason", "missing_finish_reason",
						"attempts", cur.Attempts,
						"duration_ms", time.Since(started).Milliseconds(),
						"input_tokens", inTokens,
						"output_tokens", outTokens,
						"cached_tokens", cachedTokens,
						"has_usage", hasUsage,
					)
					s.runner.RecordStreamFailure(request.Context(), logical, cur.Provider, cur.Model, broken)
					if takeover(broken) {
						continue
					}
					s.observeRequest(logical, cur.Provider, "failed", time.Since(started))
					writeStreamError(writer, flusher, broken, requestID, s.runner.streamRetryable(logical, broken))
					return
				}
				// A chat stream that closed WITH a finish_reason but delivered
				// neither text content nor a tool-call is not an answer — the
				// provider reasoned (or produced nothing) and stopped, leaving
				// the client on an empty turn (observed 2026-09-18: GLM-5.3-Flash
				// continuation relaying 3010 reasoning tokens and 0 content, and
				// carriers capping completions at 4096 tokens; both end with a
				// finish_reason so the plain sawFinishReason check cannot see
				// them). Relaying it as success makes the client finish on an
				// empty assistant message while the provider keeps a clean
				// health record, exactly the class of failure "continue" exists
				// to absorb — so surface it as a retryable outage and let the
				// takeover re-dispatch instead.
				if executeRequest.Kind == RequestChat && sawFinishReason && !partial.GotText && !partial.GotToolCalls {
					broken := &CallError{Class: ErrorUpstream, Status: http.StatusBadGateway,
						Cause: errors.New("winner stream ended with empty completion (no content, no tool calls)")}
					logEvent(request.Context(), s.logger, slog.LevelWarn, "request_failed",
						"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
						"provider", cur.Provider,
						"status_code", callErrorStatus(broken), "error_type", string(broken.Class),
						"reason", "empty_completion",
						"attempts", cur.Attempts,
						"duration_ms", time.Since(started).Milliseconds(),
						"input_tokens", inTokens,
						"output_tokens", outTokens,
						"cached_tokens", cachedTokens,
						"has_usage", hasUsage,
					)
					s.runner.RecordStreamFailure(request.Context(), logical, cur.Provider, cur.Model, broken)
					if takeover(broken) {
						continue
					}
					s.observeRequest(logical, cur.Provider, "failed", time.Since(started))
					writeStreamError(writer, flusher, broken, requestID, s.runner.streamRetryable(logical, broken))
					return
				}
				if executeRequest.Kind == RequestChat {
					_, _ = io.WriteString(writer, "data: [DONE]\n\n")
					flusher.Flush()
				}
				logEvent(request.Context(), s.logger, slog.LevelInfo, "request_completed",
					"kind", executeRequest.Kind,
					"route", s.routeOf(logical),
					"logical_model", logical,
					"provider", cur.Provider,
					"stream", true,
					"status_code", http.StatusOK,
					"status", "success",
					"duration_ms", time.Since(started).Milliseconds(),
					"ttft_ms", cur.TTFT.Milliseconds(),
					"input_tokens", inTokens,
					"output_tokens", outTokens,
					"cached_tokens", cachedTokens,
					"has_usage", hasUsage,
					"attempts", cur.Attempts,
				)
				s.observeRequest(logical, cur.Provider, "success", time.Since(started))
				return
			}
			if event.Err != nil {
				logEvent(request.Context(), s.logger, slog.LevelWarn, "request_failed",
					"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
					"provider", cur.Provider,
					"status_code", callErrorStatus(event.Err), "error_type", string(event.Err.Class),
					"attempts", cur.Attempts,
					"duration_ms", time.Since(started).Milliseconds(),
					"input_tokens", inTokens,
					"output_tokens", outTokens,
					"cached_tokens", cachedTokens,
					"has_usage", hasUsage,
				)
				// Feed the failure back into cooldown/health/lease/metrics: the
				// scheduler returned at selection, so without this the provider
				// would stay "healthy" no matter how often it breaks streams.
				s.runner.RecordStreamFailure(request.Context(), logical, cur.Provider, cur.Model, event.Err)
				if !sawFinishReason && takeover(event.Err) {
					continue
				}
				s.observeRequest(logical, cur.Provider, "failed", time.Since(started))
				writeStreamError(writer, flusher, event.Err, requestID, s.runner.streamRetryable(logical, event.Err))
				return
			}
			rearmIdle()
			if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
				return
			}
			bind(event)
			observePartial(event.Data)
			observe(event.Data)
			markFinish(event.Data)
		}
	}
}

func writeStreamEvent(writer http.ResponseWriter, flusher http.Flusher, logical string, kind RequestKind, event StreamEvent) bool {
	data, err := sanitizeJSON(event.Data, logical)
	if err != nil {
		return false
	}
	if kind == RequestResponses && event.Event != "" {
		if _, err := fmt.Fprintf(writer, "event: %s\n", event.Event); err != nil {
			return false
		}
	}
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func (s *Server) refresh(writer http.ResponseWriter, request *http.Request) {
	errorsByProvider := s.catalog.Refresh(request.Context())
	failed := make([]string, 0, len(errorsByProvider))
	for providerID := range errorsByProvider {
		failed = append(failed, providerID)
	}
	slicesSort(failed)
	status := http.StatusOK
	if len(failed) > 0 {
		status = http.StatusBadGateway
	}
	writeJSON(writer, status, map[string]any{"refreshed": len(failed) == 0, "failed_count": len(failed)})
}

func (s *Server) bindResponsesAffinity(logical, provider string, data []byte, eventType string) {
	ttl, ok := s.runner.affinityBindTTL(logical)
	if !ok || provider == "" || s.runner.affinity == nil {
		return
	}
	if eventType == "" {
		for _, id := range responseBodyAffinityIDs(data) {
			if id != "" {
				s.runner.affinity.Bind(id, provider, ttl)
			}
		}
		return
	}
	for _, id := range streamResponseAffinityIDs(data, eventType) {
		if id != "" {
			s.runner.affinity.Bind(id, provider, ttl)
		}
	}
}

func writeCallError(writer http.ResponseWriter, callErr *CallError) {
	status := callErrorStatus(callErr)
	typeName := "upstream_error"
	if callErr != nil {
		typeName = string(callErr.Class)
	}
	writeAPIError(writer, status, typeName)
}

func callErrorStatus(callErr *CallError) int {
	status := http.StatusBadGateway
	if callErr != nil {
		switch callErr.Status {
		case 400, 401, 403, 404, 408, 429, 499, 503, 504:
			status = callErr.Status
		}
	}
	if status == 499 {
		return http.StatusRequestTimeout
	}
	return status
}

func writeAPIError(writer http.ResponseWriter, status int, kind string) {
	writeJSON(writer, status, map[string]any{
		"error": map[string]any{"message": http.StatusText(status), "type": kind},
	})
}

// writeStreamError emits the terminal SSE error event of a relayed winner
// stream. Headers and the buffered prelude are already flushed at this point,
// so the SSE body is the only channel left: the payload is machine-readable
// (status_code/retryable/partial/request_id) while the message text stays the
// stable "upstream stream failed" string that text-matching clients rely on.
// Deliberately no trailing "data: [DONE]" follows: the stream did not
// complete, and a terminating marker would let clients treat the turn as
// successfully finished.
func writeStreamError(writer http.ResponseWriter, flusher http.Flusher, callErr *CallError, requestID string, retryable bool) {
	payload, _ := json.Marshal(map[string]any{
		"error": streamErrorPayload(callErr, requestID, retryable),
	})
	_, _ = fmt.Fprintf(writer, "event: error\ndata: %s\n\n", payload)
	flusher.Flush()
}

// streamErrorPayload builds the machine-readable error body of the SSE
// "event: error" frame. partial is always true: the winner was selected on a
// meaningful event, so content has already been relayed to the client before
// the failure arrived — an in-gateway retry could only duplicate it, which is
// why retrying is left to the client.
func streamErrorPayload(callErr *CallError, requestID string, retryable bool) map[string]any {
	type_ := "upstream_error"
	if callErr != nil {
		type_ = string(callErr.Class)
	}
	payload := map[string]any{
		"message":     "upstream stream failed",
		"type":        type_,
		"status_code": callErrorStatus(callErr),
		"retryable":   retryable,
		"partial":     true,
	}
	if requestID != "" {
		payload["request_id"] = requestID
	}
	return payload
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
