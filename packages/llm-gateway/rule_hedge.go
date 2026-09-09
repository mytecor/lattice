package main

// HedgeRule lets the next retry batch start after a fixed delay even while the
// current branches are still running. A parameterless hedge is not supported.
type HedgeRule struct {
	ruleBase
	After Duration `json:"after"`
}

// apply validates the hedge delay and stores it in the compiled plan.
func (r *HedgeRule) apply(ctx *stageContext) error {
	if !ctx.st.primaryRace {
		return ctx.errf("hedge requires a preceding race action")
	}
	if r.After.Duration <= 0 {
		return ctx.errf("hedge requires a positive after duration (migration from f7-09: parameterless hedge is no longer supported; add after=<delay>)")
	}
	ctx.plan.HedgeAfter = r.After.Duration
	return nil
}
