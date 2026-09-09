package main

// RaceRule is the primary route-creating action: it snapshots the pending
// candidate pool and defines the size of the initial race batch (0 means the
// whole pool). Later fallback-stage maps never change the compiled primary
// stage.
type RaceRule struct {
	ruleBase
	Count int `json:"count"`
}

// apply validates the batch size and keeps an immutable pool snapshot.
func (r *RaceRule) apply(ctx *stageContext) error {
	if ctx.st.primaryRace {
		return ctx.errf("race already declared for this model")
	}
	if !ctx.st.sawMap {
		return ctx.errf("race requires a preceding map action")
	}
	if r.Count < 0 {
		return ctx.errf("race count must not be negative")
	}
	ctx.plan.Pool = append([]Target(nil), ctx.st.pending...)
	ctx.plan.RaceCount = r.Count
	ctx.st.primaryRace = true
	return nil
}
