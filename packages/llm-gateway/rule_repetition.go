package main

// Repetition rule validation bounds and defaults. They keep the detector from
// tripping on natural speech while leaving the operator an explicit budget for
// tightening or loosening the guard on a specific logical model.
const (
	// minRepetitionRepeats is the smallest K that is a loop at all: a single
	// repeated fragment is ordinary speech, two consecutive identical
	// fragments can still be emphasis, so < 2 is meaningless.
	minRepetitionRepeats = 2
	// maxRepetitionRepeats caps K so a misconfigured rule cannot demand an
	// absurdly long run (and cannot drive tailCap below sanity).
	maxRepetitionRepeats = 100
	// minRepetitionMinLen is the smallest normalized fragment length a unit
	// may be. Sub-2 fragments are punctuation/whitespace barriers.
	minRepetitionMinLen = 2
	// defaultRepetitionRepeats is the K used when the rule omits repeats: 4
	// consecutive identical fragments is a strong loop signal while prose and
	// documented reasoning-only fingerprints stay below it.
	defaultRepetitionRepeats = 4
	// defaultRepetitionMinLen is the unit length floor used when omitted. 6
	// keeps "the the the the" / "yes yes yes yes" (unit < 6, natural fillers)
	// from tripping while catching "Tool call." (unit ~10, the observed loop).
	defaultRepetitionMinLen = 6
	// defaultRepetitionMaxLen is the unit length ceiling used when omitted.
	defaultRepetitionMaxLen = 256
	// maxRepetitionMaxLen caps MaxLen so tailCap (MaxLen*Repeats) stays a
	// bounded per-event window.
	maxRepetitionMaxLen = 1024
)

// RepetitionRule declares the in-gateway loop-guard policy for a route. It is
// an optional, opt-in typed routing action: its presence on an entry route
// arms the repetition detector on that logical model's relayed winner stream.
// Unlike continue there is no built-in loop detection in the gateway core —
// an absent rule means the relay behaves exactly as before. When the
// accumulated output of a relayed stream trips the guard (the same normalized
// fragment repeated K+ consecutive times), the gateway stops the stream and
// re-dispatches through the existing continue path with the accumulated
// partial output reshared, so the successor provider continues instead of the
// client watching an endless "Tool call. Tool call. …" loop (observed
// 2026-09-24 on the `standard` model via llm-gateway).
type RepetitionRule struct {
	ruleBase
	// Repeats is K: the number of consecutive identical normalized fragments
	// that trip the guard. Omitted/zero defaults to defaultRepetitionRepeats.
	Repeats int `json:"repeats,omitempty"`
	// MinLen is the smallest normalized fragment length considered. Omitted/
	// zero defaults to defaultRepetitionMinLen.
	MinLen int `json:"min_len,omitempty"`
	// MaxLen is the largest normalized fragment length scanned. Omitted/zero
	// defaults to defaultRepetitionMaxLen.
	MaxLen int `json:"max_len,omitempty"`
}

// apply validates the loop-guard policy and stores it in the compiled entry
// route. Like continue it must sit on an entry route after a race; it never
// enables anything by default.
func (r *RepetitionRule) apply(ctx *stageContext) error {
	if !ctx.st.sawRace {
		return ctx.errf("repetition requires a preceding race action")
	}
	if !ctx.st.entrySet {
		return ctx.errf("repetition must be declared on an entry route (the route needs a filter where.model) so the logical model and its relay policy are known")
	}
	repeats := r.Repeats
	if repeats == 0 {
		repeats = defaultRepetitionRepeats
	}
	if repeats < minRepetitionRepeats {
		return ctx.errf("repetition repeats must be at least %d, got %d", minRepetitionRepeats, repeats)
	}
	if repeats > maxRepetitionRepeats {
		return ctx.errf("repetition repeats exceeds the cap of %d, got %d", maxRepetitionRepeats, repeats)
	}
	minLen := r.MinLen
	if minLen == 0 {
		minLen = defaultRepetitionMinLen
	}
	if minLen < minRepetitionMinLen {
		return ctx.errf("repetition min_len must be at least %d, got %d", minRepetitionMinLen, minLen)
	}
	maxLen := r.MaxLen
	if maxLen == 0 {
		maxLen = defaultRepetitionMaxLen
	}
	if maxLen > maxRepetitionMaxLen {
		return ctx.errf("repetition max_len exceeds the cap of %d, got %d", maxRepetitionMaxLen, maxLen)
	}
	if minLen >= maxLen {
		return ctx.errf("repetition min_len (%d) must be strictly less than max_len (%d)", minLen, maxLen)
	}
	ctx.plan.Repetition = RepetitionConfig{
		Enabled: true,
		Repeats: repeats,
		MinLen:  minLen,
		MaxLen:  maxLen,
	}
	return nil
}
