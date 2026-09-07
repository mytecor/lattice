package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/tidwall/sjson"
)

type bifrostAccount struct {
	providers []schemas.ModelProvider
	configs   map[schemas.ModelProvider]*schemas.ProviderConfig
	keys      map[schemas.ModelProvider][]schemas.Key
}

func (a *bifrostAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return append([]schemas.ModelProvider(nil), a.providers...), nil
}

func (a *bifrostAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	config, ok := a.configs[provider]
	if !ok {
		return nil, fmt.Errorf("provider is not configured")
	}
	copy := *config
	return &copy, nil
}

func (a *bifrostAccount) GetKeysForProvider(_ context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	keys, ok := a.keys[provider]
	if !ok {
		return nil, fmt.Errorf("provider has no key configuration")
	}
	return append([]schemas.Key(nil), keys...), nil
}

type silentLogger struct{}

func (silentLogger) Debug(string, ...any)                   {}
func (silentLogger) Info(string, ...any)                    {}
func (silentLogger) Warn(string, ...any)                    {}
func (silentLogger) Error(string, ...any)                   {}
func (silentLogger) Fatal(string, ...any)                   { panic("bifrost fatal error") }
func (silentLogger) SetLevel(schemas.LogLevel)              {}
func (silentLogger) SetOutputType(schemas.LoggerOutputType) {}
func (silentLogger) LogHTTPRequest(schemas.LogLevel, string) schemas.LogEventBuilder {
	return schemas.NoopLogEvent
}

type BifrostExecutor struct {
	client    *bifrost.Bifrost
	providers map[string]Provider
}

func newBifrostExecutor(ctx context.Context, config *compiledConfig) (*BifrostExecutor, error) {
	account := &bifrostAccount{
		configs: make(map[schemas.ModelProvider]*schemas.ProviderConfig, len(config.providers)),
		keys:    make(map[schemas.ModelProvider][]schemas.Key, len(config.providers)),
	}
	for _, id := range sortedProviderIDs(config.providers) {
		provider := config.providers[id]
		providerKey := schemas.ModelProvider(provider.ID)
		baseProvider := schemas.ModelProvider(provider.BaseProvider)
		if !bifrost.IsSupportedBaseProvider(baseProvider) {
			return nil, fmt.Errorf("provider %q has unsupported Bifrost base_provider %q", provider.ID, provider.BaseProvider)
		}
		account.providers = append(account.providers, providerKey)
		account.configs[providerKey] = &schemas.ProviderConfig{
			NetworkConfig: schemas.NetworkConfig{
				BaseURL:                        provider.InferenceURL,
				ExtraHeaders:                   provider.Headers,
				DefaultRequestTimeoutInSeconds: max(1, int(provider.RequestTimeout.Duration/time.Second)),
				MaxRetries:                     provider.BifrostMaxRetries,
				AllowPrivateNetwork:            provider.AllowPrivateNetwork,
			},
			CustomProviderConfig: &schemas.CustomProviderConfig{
				BaseProviderType: baseProvider,
				IsKeyLess:        provider.APIKey == "",
			},
			SendBackRawRequest:      false,
			SendBackRawResponse:     false,
			StoreRawRequestResponse: false,
		}
		key := schemas.Key{
			ID:      "key-" + provider.ID,
			Name:    "key-" + provider.ID,
			Models:  schemas.WhiteList{"*"},
			Weight:  1,
			Enabled: schemas.Ptr(true),
		}
		if provider.APIKey != "" {
			key.Value = *schemas.NewSecretVar(provider.APIKey)
		}
		account.keys[providerKey] = []schemas.Key{key}
	}
	client, err := bifrost.Init(ctx, schemas.BifrostConfig{
		Account:         account,
		Logger:          silentLogger{},
		InitialPoolSize: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Bifrost: %w", err)
	}
	return &BifrostExecutor{client: client, providers: config.providers}, nil
}

func sortedProviderIDs(providers map[string]Provider) []string {
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	slicesSort(ids)
	return ids
}

func slicesSort(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func (e *BifrostExecutor) Do(ctx context.Context, target Target, request ExecuteRequest) ([]byte, *CallError) {
	bfContext := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	switch request.Kind {
	case RequestChat:
		chatRequest, callErr := e.chatRequest(bfContext, target, request.Body)
		if callErr != nil {
			return nil, callErr
		}
		response, bfErr := e.client.ChatCompletionRequest(bfContext, chatRequest)
		if bfErr != nil {
			return nil, classifyBifrostError(ctx, bfErr)
		}
		response.Model = target.Model
		return marshalSanitized(response, "")
	case RequestResponses:
		responsesRequest, callErr := e.responsesRequest(bfContext, target, request.Body)
		if callErr != nil {
			return nil, callErr
		}
		response, bfErr := e.client.ResponsesRequest(bfContext, responsesRequest)
		if bfErr != nil {
			return nil, classifyBifrostError(ctx, bfErr)
		}
		response.Model = target.Model
		return marshalSanitized(response, "")
	default:
		return nil, &CallError{Class: ErrorInvalid, Status: 400}
	}
}

func (e *BifrostExecutor) Stream(ctx context.Context, target Target, request ExecuteRequest) (<-chan StreamEvent, *CallError) {
	bfContext := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	var source <-chan *schemas.BifrostStreamChunk
	switch request.Kind {
	case RequestChat:
		chatRequest, callErr := e.chatRequest(bfContext, target, request.Body)
		if callErr != nil {
			return nil, callErr
		}
		stream, bfErr := e.client.ChatCompletionStreamRequest(bfContext, chatRequest)
		if bfErr != nil {
			return nil, classifyBifrostError(ctx, bfErr)
		}
		source = stream
	case RequestResponses:
		responsesRequest, callErr := e.responsesRequest(bfContext, target, request.Body)
		if callErr != nil {
			return nil, callErr
		}
		stream, bfErr := e.client.ResponsesStreamRequest(bfContext, responsesRequest)
		if bfErr != nil {
			return nil, classifyBifrostError(ctx, bfErr)
		}
		source = stream
	default:
		return nil, &CallError{Class: ErrorInvalid, Status: 400}
	}

	output := make(chan StreamEvent, 8)
	go func() {
		defer close(output)
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-source:
				if !ok {
					return
				}
				if chunk == nil {
					continue
				}
				if chunk.BifrostError != nil {
					select {
					case output <- StreamEvent{Err: classifyBifrostError(ctx, chunk.BifrostError)}:
					case <-ctx.Done():
					}
					return
				}
				data, event, meaningful, err := sanitizeStreamChunk(chunk, target.Model)
				if err != nil {
					select {
					case output <- StreamEvent{Err: &CallError{Class: ErrorInvalid, Status: 502, Cause: err}}:
					case <-ctx.Done():
					}
					return
				}
				select {
				case output <- StreamEvent{Data: data, Event: event, Meaningful: meaningful}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return output, nil
}

func (e *BifrostExecutor) chatRequest(ctx *schemas.BifrostContext, target Target, body []byte) (*schemas.BifrostChatRequest, *CallError) {
	rewritten, err := sjson.SetBytes(body, "model", target.Model)
	if err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: err}
	}
	var wire struct {
		Messages  []schemas.ChatMessage `json:"messages"`
		MaxTokens *int                  `json:"max_tokens"`
	}
	if err := json.Unmarshal(rewritten, &wire); err != nil || len(wire.Messages) == 0 {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: err}
	}
	params := &schemas.ChatParameters{}
	if err := json.Unmarshal(rewritten, params); err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: err}
	}
	if params.MaxCompletionTokens == nil && wire.MaxTokens != nil {
		params.MaxCompletionTokens = wire.MaxTokens
	}
	request := &schemas.BifrostChatRequest{
		Provider: schemas.ModelProvider(target.Provider),
		Model:    target.Model,
		Input:    wire.Messages,
		Params:   params,
	}
	if e.providers[target.Provider].BaseProvider == string(schemas.OpenAI) {
		request.RawRequestBody = rewritten
		ctx.SetValue(schemas.BifrostContextKeyUseRawRequestBody, true)
	}
	return request, nil
}

func (e *BifrostExecutor) responsesRequest(ctx *schemas.BifrostContext, target Target, body []byte) (*schemas.BifrostResponsesRequest, *CallError) {
	rewritten, err := sjson.SetBytes(body, "model", target.Model)
	if err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: err}
	}
	var wire struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(rewritten, &wire); err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: err}
	}
	input, err := parseResponsesInput(wire.Input)
	if err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: err}
	}
	params := &schemas.ResponsesParameters{}
	if err := json.Unmarshal(rewritten, params); err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 400, Cause: err}
	}
	request := &schemas.BifrostResponsesRequest{
		Provider: schemas.ModelProvider(target.Provider),
		Model:    target.Model,
		Input:    input,
		Params:   params,
	}
	if e.providers[target.Provider].BaseProvider == string(schemas.OpenAI) {
		request.RawRequestBody = rewritten
		ctx.SetValue(schemas.BifrostContextKeyUseRawRequestBody, true)
	}
	return request, nil
}

func parseResponsesInput(raw json.RawMessage) ([]schemas.ResponsesMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, errors.New("input is required")
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		messageType := schemas.ResponsesMessageTypeMessage
		role := schemas.ResponsesInputMessageRoleUser
		return []schemas.ResponsesMessage{{
			Type:    &messageType,
			Role:    &role,
			Content: &schemas.ResponsesMessageContent{ContentStr: &text},
		}}, nil
	}
	var messages []schemas.ResponsesMessage
	if err := json.Unmarshal(trimmed, &messages); err != nil {
		return nil, fmt.Errorf("input must be a string or array: %w", err)
	}
	if len(messages) == 0 {
		return nil, errors.New("input must not be empty")
	}
	return messages, nil
}

func classifyBifrostError(ctx context.Context, err *schemas.BifrostError) *CallError {
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &CallError{Class: ErrorTimeout, Status: 504, Cause: ctx.Err()}
		}
		return &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}
	}
	status := 502
	if err != nil && err.StatusCode != nil {
		status = *err.StatusCode
	}
	switch {
	case status == 429:
		return &CallError{Class: ErrorRateLimit, Status: status}
	case status == 408 || status == 504:
		return &CallError{Class: ErrorTimeout, Status: status}
	case status >= 500:
		return &CallError{Class: ErrorUpstream, Status: status}
	}
	message := ""
	if err != nil && err.Error != nil {
		message = strings.ToLower(err.Error.Message)
	}
	if strings.Contains(message, "timeout") || strings.Contains(message, "deadline") {
		return &CallError{Class: ErrorTimeout, Status: 504}
	}
	if strings.Contains(message, "connection") || strings.Contains(message, "network") || strings.Contains(message, "dns") {
		return &CallError{Class: ErrorConnection, Status: 502}
	}
	return &CallError{Class: ErrorInvalid, Status: status}
}

func marshalSanitized(value any, logicalModel string) ([]byte, *CallError) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 502, Cause: err}
	}
	data, err = sanitizeJSON(data, logicalModel)
	if err != nil {
		return nil, &CallError{Class: ErrorInvalid, Status: 502, Cause: err}
	}
	return data, nil
}

func sanitizeStreamChunk(chunk *schemas.BifrostStreamChunk, nativeModel string) ([]byte, string, bool, error) {
	event := ""
	if chunk.BifrostResponsesStreamResponse != nil {
		event = string(chunk.BifrostResponsesStreamResponse.Type)
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return nil, "", false, err
	}
	data, err = sanitizeJSON(data, nativeModel)
	if err != nil {
		return nil, "", false, err
	}
	return data, event, meaningfulPayload(data, event), nil
}

func sanitizeJSON(data []byte, model string) ([]byte, error) {
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	delete(object, "extra_fields")
	delete(object, "provider_extra_fields")
	if model != "" {
		if _, exists := object["model"]; exists {
			object["model"] = model
		}
		if response, ok := object["response"].(map[string]any); ok {
			response["model"] = model
			delete(response, "extra_fields")
			delete(response, "provider_extra_fields")
		}
	}
	return json.Marshal(object)
}

func meaningfulPayload(data []byte, event string) bool {
	if event != "" {
		switch event {
		case "response.output_text.delta", "response.reasoning_summary_text.delta",
			"response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
			var object map[string]any
			if json.Unmarshal(data, &object) == nil {
				if delta, ok := object["delta"].(string); ok {
					return delta != ""
				}
			}
		}
		return false
	}
	var object struct {
		Choices []struct {
			Delta map[string]json.RawMessage `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &object) != nil {
		return false
	}
	for _, choice := range object.Choices {
		for _, key := range []string{"content", "reasoning", "reasoning_content", "tool_calls"} {
			raw := bytes.TrimSpace(choice.Delta[key])
			if len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && !bytes.Equal(raw, []byte(`""`)) && !bytes.Equal(raw, []byte("[]")) {
				return true
			}
		}
	}
	return false
}

func (e *BifrostExecutor) Close() error {
	if e.client != nil {
		e.client.Shutdown()
	}
	return nil
}
