package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxRequestBody = 16 << 20

type Server struct {
	config  *compiledConfig
	catalog *Catalog
	runner  *Runner
	mux     *http.ServeMux
}

func newServer(config *compiledConfig, catalog *Catalog, runner *Runner) *Server {
	server := &Server{config: config, catalog: catalog, runner: runner, mux: http.NewServeMux()}
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
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var metadata struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil || strings.TrimSpace(metadata.Model) == "" {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, exists := s.config.plans[metadata.Model]; !exists {
		writeAPIError(writer, http.StatusNotFound, "model_not_found")
		return
	}
	executeRequest := ExecuteRequest{Kind: kind, Body: body}
	if metadata.Stream {
		s.stream(writer, request, metadata.Model, executeRequest)
		return
	}
	response, callErr := s.runner.Run(request.Context(), metadata.Model, executeRequest)
	if callErr != nil {
		writeCallError(writer, callErr)
		return
	}
	response, err = sanitizeJSON(response, metadata.Model)
	if err != nil {
		writeAPIError(writer, http.StatusBadGateway, "invalid_upstream_response")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(response)
}

func (s *Server) stream(writer http.ResponseWriter, request *http.Request, logical string, executeRequest ExecuteRequest) {
	selected, callErr := s.runner.SelectStream(request.Context(), logical, executeRequest)
	if callErr != nil {
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
	for _, event := range selected.Buffered {
		if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
			return
		}
	}
	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-selected.Remaining:
			if !open {
				if executeRequest.Kind == RequestChat {
					_, _ = io.WriteString(writer, "data: [DONE]\n\n")
					flusher.Flush()
				}
				return
			}
			if event.Err != nil {
				payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "upstream stream failed", "type": string(event.Err.Class)}})
				_, _ = fmt.Fprintf(writer, "event: error\ndata: %s\n\n", payload)
				flusher.Flush()
				return
			}
			if !writeStreamEvent(writer, flusher, logical, executeRequest.Kind, event) {
				return
			}
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
	errorsByGroup := s.catalog.Refresh(request.Context())
	failed := make([]string, 0, len(errorsByGroup))
	for group := range errorsByGroup {
		failed = append(failed, group)
	}
	slicesSort(failed)
	status := http.StatusOK
	if len(failed) > 0 {
		status = http.StatusBadGateway
	}
	writeJSON(writer, status, map[string]any{"refreshed": len(failed) == 0, "failed_count": len(failed)})
}

func writeCallError(writer http.ResponseWriter, callErr *CallError) {
	status := http.StatusBadGateway
	if callErr != nil {
		switch callErr.Status {
		case 400, 401, 403, 404, 408, 429, 499, 503, 504:
			status = callErr.Status
		}
	}
	if status == 499 {
		status = 408
	}
	typeName := "upstream_error"
	if callErr != nil {
		typeName = string(callErr.Class)
	}
	writeAPIError(writer, status, typeName)
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
