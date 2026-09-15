package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
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
	defer selected.Cancel()
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
	observe := func(data []byte) {
		if summary, ok := extractUsageFull(data); ok {
			inTokens += summary.Input
			outTokens += summary.Output
			cachedTokens += summary.CachedTokens
			hasUsage = true
			s.metrics.ObserveTokens(logical, summary.Input, summary.Output)
		}
	}
	for _, event := range selected.Buffered {
		bind(event)
		observe(event.Data)
		if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
			return
		}
	}
	for {
		select {
		case <-request.Context().Done():
			logEvent(request.Context(), s.logger, slog.LevelDebug, "request_cancelled",
				"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
				"provider", selected.Provider,
				"attempts", selected.Attempts,
				"duration_ms", time.Since(started).Milliseconds(),
			)
			s.observeRequest(logical, selected.Provider, "cancelled", time.Since(started))
			return
		case event, open := <-selected.Remaining:
			if !open {
				if executeRequest.Kind == RequestChat {
					_, _ = io.WriteString(writer, "data: [DONE]\n\n")
					flusher.Flush()
				}
				logEvent(request.Context(), s.logger, slog.LevelInfo, "request_completed",
					"kind", executeRequest.Kind,
					"route", s.routeOf(logical),
					"logical_model", logical,
					"provider", selected.Provider,
					"stream", true,
					"status_code", http.StatusOK,
					"status", "success",
					"duration_ms", time.Since(started).Milliseconds(),
					"ttft_ms", selected.TTFT.Milliseconds(),
					"input_tokens", inTokens,
					"output_tokens", outTokens,
					"cached_tokens", cachedTokens,
					"has_usage", hasUsage,
					"attempts", selected.Attempts,
				)
				s.observeRequest(logical, selected.Provider, "success", time.Since(started))
				return
			}
			if event.Err != nil {
				logEvent(request.Context(), s.logger, slog.LevelWarn, "request_failed",
					"kind", executeRequest.Kind, "logical_model", logical, "stream", true,
					"provider", selected.Provider,
					"status_code", callErrorStatus(event.Err), "error_type", string(event.Err.Class),
					"attempts", selected.Attempts,
					"duration_ms", time.Since(started).Milliseconds(),
				)
				s.observeRequest(logical, selected.Provider, "failed", time.Since(started))
				payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "upstream stream failed", "type": string(event.Err.Class)}})
				_, _ = fmt.Fprintf(writer, "event: error\ndata: %s\n\n", payload)
				flusher.Flush()
				return
			}
			if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
				return
			}
			bind(event)
			observe(event.Data)
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

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
