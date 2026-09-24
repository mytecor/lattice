package main

import (
	"encoding/json"
	"strings"
	"unicode"
)

// streamTextDelta extracts the streamed text of one OpenAI chat-completion
// SSE chunk: the delta's content and (when present) reasoning/reasoning_content
// concatenated in stream order without a separator, and "" when the chunk
// carries no text (a finish event, a tool-call-only delta, non-chat shape, or
// unparsable JSON).
//
// The loop guard must be fed the actual streamed text, not the raw SSE JSON
// envelope: the repeating fragment "Tool call." lives as delta.content inside
// JSON that also carries fixed structural characters (",", ":", "[") which are
// not loop separators and would otherwise break the normalized-unit cadence.
func streamTextDelta(data string) string {
	var envelope struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				Reasoning        string `json:"reasoning"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return ""
	}
	if len(envelope.Choices) == 0 {
		return ""
	}
	delta := envelope.Choices[0].Delta
	text := delta.Content
	if reasoning := delta.Reasoning; reasoning != "" {
		text += reasoning
	} else if reasoning := delta.ReasoningContent; reasoning != "" {
		text += reasoning
	}
	return text
}

// Loop separators are collapsed to a single separator when building the
// normalized fragment stream: any whitespace run and common sentence
// punctuation fuse to one space. This makes case- and punctuation-insensitive
// repetition ("Tool call." vs "tool call." vs "Tool call!") all normalize to
// the same unit so the detector is not defeated by trivial variation, while a
// genuine phrase loop still surfaces as repeated identical normalized units.
func isLoopSeparator(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '.', ',', '!', '?', ';', ':', '"', '\'':
		return true
	}
	return false
}

// repetitionDetector implements the loop-guard policy of the "repetition"
// routing rule: it watches the accumulated normalized output of a relayed
// stream and reports when the tail becomes K consecutive identical normalized
// fragments. It is a pure, standalone component with no knowledge of the relay
// loop or the request lifecycle: the relay loop owns its lifecycle and only
// asks Detect after each relayed chunk. Memory is bounded: it keeps only the
// normalized tail needed to answer the current check, never the whole turn.
type repetitionDetector struct {
	cfg          RepetitionConfig
	norm         string // accumulated normalized text (kept to tailCap)
	tailCap      int    // MaxLen*Repeats: how much normalized tail to retain
	spacePending bool   // a separator seen since the last emitted rune
}

// newRepetitionDetector builds a detector for the given compiled policy.
func newRepetitionDetector(cfg RepetitionConfig) *repetitionDetector {
	return &repetitionDetector{
		cfg:     cfg,
		tailCap: cfg.MaxLen * cfg.Repeats,
	}
}

// Reset clears the accumulated normalized state (a fresh lane starts clean;
// the relay currently keeps one detector across a whole request, but a caller
// that wants per-lane detection can Reset on adoption).
func (d *repetitionDetector) Reset() {
	d.norm = ""
	d.spacePending = false
}

// appendNormalized folds one raw chunk into the normalized accumulator,
// collapsing separators (including across chunk boundaries) and trimming to
// the bounded tail window.
func (d *repetitionDetector) appendNormalized(raw string) {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.ToLower(raw) {
		if isLoopSeparator(r) {
			d.spacePending = true
			continue
		}
		if d.spacePending {
			if len(d.norm) > 0 || b.Len() > 0 {
				b.WriteByte(' ')
			}
			d.spacePending = false
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		d.norm += b.String()
	}
	if len(d.norm) > d.tailCap {
		d.norm = d.norm[len(d.norm)-d.tailCap:]
	}
}

// Detect folds one raw relayed chunk into the normalized accumulator and
// reports whether the accumulated output has just tripped the loop guard
// (the tail is K consecutive identical normalized fragments of a unit length
// within [MinLen, MaxLen]).
func (d *repetitionDetector) Detect(raw string) bool {
	if !d.cfg.Enabled {
		return false
	}
	d.appendNormalized(raw)
	return repeatedRun(d.norm, d.cfg.Repeats, d.cfg.MinLen, d.cfg.MaxLen)
}

// repeatedRun reports whether s ends with (>= repeats) consecutive identical
// normalized units, where the minimal such unit has length u in [minLen,
// maxLen]. It examines only the bounded tail, so per-event cost is
// O(maxLen*repeats) regardless of how long the turn has grown.
//
// Because the accumulator collapses every separator to a single space, a run
// is `unit (space unit)^(repeats-1)` and each preceding occurrence sits at
// stride (u+1) back from the tail. A unit is taken from the last u chars; any
// u that reproduces the tail K times is a candidate run.
//
// The guard is "the minimal fundamental": if the tail ALSO decomposes into a
// run with a unit shorter than minLen, it is ordinary repeated speech
// (fillers, laughter, single words, numbers) rather than a loop, and is
// rejected no matter how many copies accumulate. This keeps a stream of
// "haha haha haha …" (minimal unit 4 < min_len 6) from being re-read as a
// longer unit while still catching "Tool call. Tool call. …" (minimal unit 9
// >= min_len 6). Requiring the run to end at the stream tail keeps the guard
// on "the text currently being emitted", which is exactly the looping signal:
// a model mid-loop emits the same fragment over and over, so the last K align.
func repeatedRun(s string, repeats, minLen, maxLen int) bool {
	if repeats < 2 || minLen < 1 || maxLen < minLen {
		return false
	}
	n := len(s)
	if n <= 0 {
		return false
	}
	tailEnd := n
	// The widest tail we scan is bounded by maxLen*repeats plus the single
	// separator between consecutive units; clamp to the available text.
	widest := maxLen*repeats + (repeats - 1)
	if widest > n {
		widest = n
	}
	// A run needs at least minLen*repeats + (repeats-1) bytes to exist.
	if widest < minLen*repeats+(repeats-1) {
		return false
	}
	tailStart := n - widest
	// runAt reports whether the tail is K identical units of exactly u bytes,
	// each internal unit followed by (and hence preceded by) a single
	// collapsed separator. The first unit of the run may start exactly at the
	// window edge with no visible separator — still K identical units.
	runAt := func(u int) bool {
		unit := s[tailEnd-u : tailEnd]
		for m := 1; m < repeats; m++ {
			unitStart := tailEnd - (u+1)*m - u
			if unitStart > tailStart && s[unitStart-1] != ' ' {
				return false
			}
			if unitStart < tailStart || s[unitStart:unitStart+u] != unit {
				return false
			}
		}
		return true
	}
	// A run whose minimal unit is below minLen is ordinary repeated speech, not
	// a loop — reject regardless of any longer unit also matching. The first
	// matching u in ascending order is the minimal unit.
	for u := 1; u < minLen; u++ {
		if runAt(u) {
			return false
		}
	}
	// The loop signal: K identical units of some in-range unit length.
	for u := minLen; u <= maxLen; u++ {
		if u*repeats+(repeats-1) > widest {
			break
		}
		if runAt(u) {
			return true
		}
	}
	return false
}
