package main

// TimeoutRule bounds the entire route graph (all transitions included) with
// an absolute deadline. It is request-wide and may be declared only on an
// entry route, so backoff and every subroute consume the same timeout budget.
type TimeoutRule struct {
	ruleBase
	Duration Duration `json:"duration"`
}

// apply validates the route timeout and stores it in the compiled entry route.
func (r *TimeoutRule) apply(ctx *stageContext) error {
	if !ctx.st.sawRace {
		return ctx.errf("timeout requires a preceding race action")
	}
	if r.Duration.Duration <= 0 {
		return ctx.errf("timeout duration must be positive")
	}
	ctx.plan.RouteTimeout = r.Duration.Duration
	return nil
}
