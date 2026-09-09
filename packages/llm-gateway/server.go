package main

import (
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
	requestSequence atomic.Uint64
}

func newServer(config *compiledConfig, catalog *Catalog, runner *Runner) *Server {
	server := &Server{
		config: config, catalog: catalog, runner: runner, mux: http.NewServeMux(), logger: config.logger,
	}
	server.mux.HandleFunc("GET /healthz", server.health)
	server.mux.HandleFunc("GET /v1/models", server.auth(server.models))
	server.mux.HandleFunc("POST /v1/chat/completions", server.auth(server.chat))
	server.mux.HandleFunc("POST /v1/responses", server.auth(server.responses))
	server.mux.HandleFunc("POST /admin/models/refresh", server.auth(server.refresh))
	return server
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.mux.ServeHTTP(writer, request)
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
		s.logger.Warn("request rejected", "request_id", requestID, "kind", kind, "status", http.StatusBadRequest, "reason", "invalid_body")
		writeAPIError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var metadata struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil || strings.TrimSpace(metadata.Model) == "" {
		s.logger.Warn("request rejected", "request_id", requestID, "kind", kind, "status", http.StatusBadRequest, "reason", "invalid_json_or_model")
		writeAPIError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, exists := s.config.plans[metadata.Model]; !exists {
		s.logger.Warn("request rejected", "request_id", requestID, "kind", kind, "logical_model", metadata.Model, "status", http.StatusNotFound, "reason", "model_not_found")
		writeAPIError(writer, http.StatusNotFound, "model_not_found")
		return
	}
	s.logger.Info("request started", "request_id", requestID, "kind", kind, "logical_model", metadata.Model, "stream", metadata.Stream)
	executeRequest := ExecuteRequest{Kind: kind, Body: body}
	if metadata.Stream {
		s.stream(writer, request, metadata.Model, executeRequest, started)
		return
	}
	result, callErr := s.runner.RunWithResult(request.Context(), metadata.Model, executeRequest)
	if callErr != nil {
		s.logger.Warn("request failed",
			"request_id", requestID, "kind", kind, "logical_model", metadata.Model,
			"status", callErrorStatus(callErr), "error_class", callErr.Class,
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeCallError(writer, callErr)
		return
	}
	response := result.Body
	// Bind the winning provider to the opaque Responses state identifiers;
	// the identifiers themselves and the request body never reach the logs.
	if kind == RequestResponses {
		s.bindResponsesAffinity(metadata.Model, result.Provider, response, "")
	}
	response, err = sanitizeJSON(response, metadata.Model)
	if err != nil {
		s.logger.Error("request failed", "request_id", requestID, "kind", kind, "logical_model", metadata.Model, "status", http.StatusBadGateway, "error_class", ErrorInvalid, "duration_ms", time.Since(started).Milliseconds())
		writeAPIError(writer, http.StatusBadGateway, "invalid_upstream_response")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(response)
	s.logger.Info("request completed", "request_id", requestID, "kind", kind, "logical_model", metadata.Model, "status", http.StatusOK, "duration_ms", time.Since(started).Milliseconds())
}

func (s *Server) stream(writer http.ResponseWriter, request *http.Request, logical string, executeRequest ExecuteRequest, started time.Time) {
	selected, callErr := s.runner.SelectStream(request.Context(), logical, executeRequest)
	if callErr != nil {
		s.logger.Warn("request failed",
			"request_id", request.Context().Value(requestIDKey), "kind", executeRequest.Kind, "logical_model", logical,
			"status", callErrorStatus(callErr), "error_class", callErr.Class,
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
	for _, event := range selected.Buffered {
		bind(event)
		if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
			return
		}
	}
	for {
		select {
		case <-request.Context().Done():
			s.logger.Debug("request cancelled", "request_id", request.Context().Value(requestIDKey), "kind", executeRequest.Kind, "logical_model", logical, "duration_ms", time.Since(started).Milliseconds())
			return
		case event, open := <-selected.Remaining:
			if !open {
				if executeRequest.Kind == RequestChat {
					_, _ = io.WriteString(writer, "data: [DONE]\n\n")
					flusher.Flush()
				}
				s.logger.Info("request completed", "request_id", request.Context().Value(requestIDKey), "kind", executeRequest.Kind, "logical_model", logical, "status", http.StatusOK, "duration_ms", time.Since(started).Milliseconds())
				return
			}
			if event.Err != nil {
				s.logger.Warn("request stream failed", "request_id", request.Context().Value(requestIDKey), "kind", executeRequest.Kind, "logical_model", logical, "error_class", event.Err.Class, "duration_ms", time.Since(started).Milliseconds())
				payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "upstream stream failed", "type": string(event.Err.Class)}})
				_, _ = fmt.Fprintf(writer, "event: error\ndata: %s\n\n", payload)
				flusher.Flush()
				return
			}
			if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
				return
			}
			bind(event)
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
