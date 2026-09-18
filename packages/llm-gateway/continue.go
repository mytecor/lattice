package main

import (
	"bytes"
	"encoding/json"
)

// partialStreamOutput accumulates the assistant side of a relayed chat
// completion stream whose winner provider stalled or broke before a
// finish_reason. The accumulated deltas are what the in-gateway continuation
// (the "continue" routing rule) reshapes into an assistant context message
// appended to the request history, so a stakeholder provider can continue the
// answer instead of starting from scratch.
type partialStreamOutput struct {
	Content   string
	Reasoning string
	GotText   bool
	// GotToolCalls reports whether the relayed stream carried any tool-call
	// delta (choices[].delta.tool_calls non-empty). A chat completion that ends
	// with a finish_reason but delivered neither text content nor a tool-call
	// is an empty answer (observed 2026-09-18: GLM-5.3-Flash finishing after
	// reasoning-only, 3010 reasoning tokens, 0 content) and must not be relayed
	// as a successful completion; a tool-call completion, by contrast, is a
	// legitimate turn that must keep relaying as success.
	GotToolCalls bool
}

// accumulatePartial folds one relayed chat-completion chunk (as sanitized by
// the gateway, i.e. an OpenAI chat.completion.chunk JSON object) into the
// accumulated partial output. It deliberately ignores usage-only and
// finish-marker chunks: they carry no assistant text. Each chunk is assumed to
// carry a single choices[0].delta (the gateway relays winner chunks
// individually), matching how relayed deltas are already parsed by
// extractFinishReason/extractUsageFull.
func accumulatePartial(body []byte, out *partialStreamOutput) {
	if len(body) == 0 {
		return
	}
	var envelope struct {
		Choices []struct {
			Delta struct {
				Content          string          `json:"content"`
				Reasoning        string          `json:"reasoning"`
				ReasoningContent string          `json:"reasoning_content"`
				ToolCalls        json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return
	}
	if len(envelope.Choices) == 0 {
		return
	}
	delta := envelope.Choices[0].Delta
	if delta.Content != "" {
		out.Content += delta.Content
		out.GotText = true
	}
	if len(delta.ToolCalls) > 0 && string(delta.ToolCalls) != "[]" && string(delta.ToolCalls) != "null" {
		out.GotToolCalls = true
	}
	reasoning := delta.Reasoning
	if reasoning == "" {
		reasoning = delta.ReasoningContent
	}
	if reasoning != "" {
		out.Reasoning += reasoning
	}
}

// appendPartialChatHistory returns a new chat-completions request body whose
// messages end with an assistant message carrying the accumulated partial
// output (content and, when present, reasoning_content), preserving the whole
// original request untouched. It operates only on the OpenAI chat shape; a
// body that cannot be parsed or is not a chat body is returned unchanged with
// a nil error so the caller can still decide what to do with a continuation
// that failed to build.
func appendPartialChatHistory(body []byte, partial *partialStreamOutput) ([]byte, error) {
	if partial == nil || !partial.GotText {
		return body, nil
	}
	var envelope struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return body, nil
	}
	if len(envelope.Messages) == 0 || string(envelope.Messages) == "null" {
		return body, nil
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(envelope.Messages, &messages); err != nil {
		return body, nil
	}
	assistant := map[string]any{"role": "assistant"}
	assistant["content"] = partial.Content
	if partial.Reasoning != "" {
		assistant["reasoning_content"] = partial.Reasoning
	}
	assistantRaw, err := json.Marshal(assistant)
	if err != nil {
		return body, nil
	}
	messages = append(messages, assistantRaw)
	rewritten, err := rewriteMessagesField(body, messages)
	if err != nil {
		return body, nil
	}
	return rewritten, nil
}

// rewriteMessagesField replaces the top-level messages array of a
// chat-completions body with the given raw messages, preserving every other
// field byte-for-byte (such as provider-specific controls the client sent,
// e.g. a zai thinking block that strip_params handling may care about).
func rewriteMessagesField(body []byte, messages []json.RawMessage) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	object["messages"] = encoded
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(object); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
