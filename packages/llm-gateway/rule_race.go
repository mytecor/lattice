package main

// RaceRule is the route-creating action: it snapshots the route pending
// candidate pool and defines the size of the race batch (0 means the whole
// pool).
type RaceRule struct {
	ruleBase
	Count int `json:"count"`
}

// apply validates the batch size and keeps an immutable pool snapshot.
func (r *RaceRule) apply(ctx *stageContext) error {
	if ctx.st.sawRace {
		return ctx.errf("race already declared for route %q", ctx.route)
	}
	if !ctx.st.sawMap {
		return ctx.errf("race requires a preceding map action")
	}
	if r.Count < 0 {
		return ctx.errf("race count must not be negative")
	}
	if len(ctx.st.pending) == 0 {
		return ctx.errf("race requires at least one candidate provider")
	}
	if r.Count > len(ctx.st.pending) {
		return ctx.errf("race count %d exceeds the route candidate pool of %d providers", r.Count, len(ctx.st.pending))
	}
	ctx.plan.Pool = append(ctx.plan.Pool, ctx.st.pending...)
	ctx.plan.RaceCount = r.Count
	ctx.st.sawRace = true
	return nil
}
