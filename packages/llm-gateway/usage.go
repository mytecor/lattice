package main

import "encoding/json"

// usageSummary is the parsed token usage of one provider response (non-stream
// body or a stream usage chunk). CachedTokens is optional and zero when the
// provider does not report prompt-cache hits; it is only populated for the
// Responses API shape (top-level cached_tokens).
type usageSummary struct {
	Input        int64
	Output       int64
	CachedTokens int64
}

// extractUsage parses token usage from a non-stream provider response body and
// also reports the prompt-cache hit count when the provider supplies it. It
// tolerates both OpenAI Chat Completions usage
// (prompt_tokens / completion_tokens) and Responses API usage
// (input_tokens / output_tokens / cached_tokens). The boolean report is false
// when a provider omits usage entirely (a legitimate response), so the caller
// contributes zero tokens without inventing them. The expensive responses
// shape is handled by reading the top-level usage object only; detail data
// (audio/text/input_details) is intentionally ignored.
func extractUsage(body []byte) (input, output int64, ok bool) {
	summary, ok := extractUsageFull(body)
	if !ok {
		return 0, 0, false
	}
	return summary.Input, summary.Output, true
}

// extractUsageFull is extractUsage plus the cached-token count, used by the
// structured events path. Metrics keep observing through extractUsage so the
// token surface stays unchanged there.
func extractUsageFull(body []byte) (usage usageSummary, ok bool) {
	if len(body) == 0 {
		return usageSummary{}, false
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
		return usageSummary{}, false
	}
	var raw struct {
		PromptTokens     json.Number `json:"prompt_tokens"`
		CompletionTokens json.Number `json:"completion_tokens"`
		InputTokens      json.Number `json:"input_tokens"`
		OutputTokens     json.Number `json:"output_tokens"`
		CachedTokens     json.Number `json:"cached_tokens"`
	}
	if err := json.Unmarshal(envelope.Usage, &raw); err != nil {
		return usageSummary{}, false
	}
	if n, err := raw.PromptTokens.Int64(); err == nil {
		usage.Input += n
	}
	if n, err := raw.InputTokens.Int64(); err == nil {
		usage.Input += n
	}
	if n, err := raw.CompletionTokens.Int64(); err == nil {
		usage.Output += n
	}
	if n, err := raw.OutputTokens.Int64(); err == nil {
		usage.Output += n
	}
	if n, err := raw.CachedTokens.Int64(); err == nil {
		usage.CachedTokens += n
	}
	return usage, true
}
