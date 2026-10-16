package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// partialToolCall tracks one streamed tool-call entry by its choice delta
// index across chunks of a relayed chat completion stream.
type partialToolCall struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

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
	ToolCalls    []partialToolCall
	LastChunkID  string
}

// HasCompleteToolCalls reports whether the relayed stream carried at least one
// tool call and every started tool call has a function name and a complete,
// syntactically valid JSON arguments object. When an upstream provider (such
// as Vertex AI / Gemini, which emits each functionCall with complete JSON args
// in one chunk and sends finishReason on a separate trailing chunk) stalls or
// drops the connection after delivering complete tool calls, the gateway can
// synthesize the missing finish_reason:"tool_calls" chunk and close the turn
// cleanly with [DONE] instead of surfacing an error. A tool call whose
// arguments JSON was cut mid-stream (e.g. `{"path":"/tmp/x.go","`) returns
// false so a truncated call is never marked complete.
func (out *partialStreamOutput) HasCompleteToolCalls() bool {
	if out == nil || !out.GotToolCalls || len(out.ToolCalls) == 0 {
		return false
	}
	for _, tc := range out.ToolCalls {
		if strings.TrimSpace(tc.Name) == "" {
			return false
		}
		args := strings.TrimSpace(tc.Arguments)
		if len(args) < 2 || args[0] != '{' || args[len(args)-1] != '}' || !json.Valid([]byte(args)) {
			return false
		}
	}
	return true
}

func synthesizeToolCallFinishChunk(chunkID, logicalModel string) []byte {
	if chunkID == "" {
		chunkID = "chatcmpl-continued"
	}
	payload := map[string]any{
		"id":      chunkID,
		"object":  "chat.completion.chunk",
		"model":   logicalModel,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
	}
	data, _ := json.Marshal(payload)
	return data
}

// accumulatePartial folds one relayed chat-completion chunk (as sanitized by
// the gateway, i.e. an OpenAI chat.completion.chunk JSON object) into the
// accumulated partial output. It deliberately ignores usage-only and
// finish-marker chunks: they carry no assistant text. Each chunk is assumed to
// carry a single choices[0].delta (the gateway relays winner chunks
// individually), matching how relayed deltas are already parsed by
// extractFinishReason/extractUsageFull.
func accumulatePartial(body []byte, out *partialStreamOutput) {
	if len(body) == 0 || out == nil {
		return
	}
	var envelope struct {
		ID      string `json:"id"`
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
	if envelope.ID != "" {
		out.LastChunkID = envelope.ID
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
		var calls []struct {
			Index    *int   `json:"index"`
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if err := json.Unmarshal(delta.ToolCalls, &calls); err == nil {
			for _, tc := range calls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				found := -1
				for i := range out.ToolCalls {
					if out.ToolCalls[i].Index == idx {
						found = i
						break
					}
				}
				if found == -1 {
					out.ToolCalls = append(out.ToolCalls, partialToolCall{Index: idx})
					found = len(out.ToolCalls) - 1
				}
				if tc.ID != "" {
					out.ToolCalls[found].ID = tc.ID
				}
				if tc.Function.Name != "" {
					out.ToolCalls[found].Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					out.ToolCalls[found].Arguments += tc.Function.Arguments
				}
			}
		}
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
	// A partial that already carried a tool-call delta must never be reshared
	// as plain assistant text: the relayed stream was cut mid tool-call (or at
	// a completed tool-call whose finish_reason never arrived), and its
	// arguments JSON is either truncated or, at best, unfit to hand to a
	// different provider as prose. Re-dispatching with that prose in context
	// makes the successor provider echo the dangling tool-call as text and
	// emit an empty/truncated structured tool-call — the "tool-call written
	// into the text with an empty call block below" symptom (observed on
	// `standard`). Leave the request untouched so the caller can surface a
	// clean retryable error instead of a malformed continuation.
	if partial == nil || (!partial.GotText && partial.Reasoning == "") || partial.GotToolCalls {
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
