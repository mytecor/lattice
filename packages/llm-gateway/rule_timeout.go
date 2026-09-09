package main

// TimeoutRule bounds the entire compiled route (primary and fallback stages)
// with an absolute deadline.
type TimeoutRule struct {
	ruleBase
	Duration Duration `json:"duration"`
}

// apply validates the route timeout and stores it in the compiled plan.
func (r *TimeoutRule) apply(ctx *stageContext) error {
	if !ctx.st.primaryRace {
		return ctx.errf("timeout requires a preceding race action")
	}
	if r.Duration.Duration <= 0 {
		return ctx.errf("timeout duration must be positive")
	}
	ctx.plan.RouteTimeout = r.Duration.Duration
	return nil
}
