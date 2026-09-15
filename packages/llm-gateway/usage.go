package main

import "encoding/json"

// extractUsage parses token usage from a non-stream provider response body.
// It tolerates both OpenAI Chat Completions usage
// (prompt_tokens / completion_tokens) and Responses API usage
// (input_tokens / output_tokens / cached_tokens). The boolean report is false
// when a provider omits usage entirely (a legitimate response), so the caller
// contributes zero tokens without inventing them. The expensive responses
// shape is handled by reading the top-level usage object only; detail data
// (audio/text/input_details) is intentionally ignored.
func extractUsage(body []byte) (input, output int64, ok bool) {
	if len(body) == 0 {
		return 0, 0, false
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
		return 0, 0, false
	}
	var usage struct {
		PromptTokens     json.Number `json:"prompt_tokens"`
		CompletionTokens json.Number `json:"completion_tokens"`
		InputTokens      json.Number `json:"input_tokens"`
		OutputTokens     json.Number `json:"output_tokens"`
	}
	if err := json.Unmarshal(envelope.Usage, &usage); err != nil {
		return 0, 0, false
	}
	if n, err := usage.PromptTokens.Int64(); err == nil {
		input += n
	}
	if n, err := usage.InputTokens.Int64(); err == nil {
		input += n
	}
	if n, err := usage.CompletionTokens.Int64(); err == nil {
		output += n
	}
	if n, err := usage.OutputTokens.Int64(); err == nil {
		output += n
	}
	return input, output, true
}
