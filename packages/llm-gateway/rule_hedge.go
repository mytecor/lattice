package main

import "strings"

// HedgeRule lets a hedge branch start after a fixed delay even while
// the current route's branches are still running: either an unused candidate in
// the current pool (when target is omitted) or an explicit target route.
type HedgeRule struct {
	ruleBase
	After  Duration `json:"after"`
	Target string   `json:"target,omitempty"`
}

// apply validates the hedge delay and target and stores them in the compiled
// route.
func (r *HedgeRule) apply(ctx *stageContext) error {
	if !ctx.st.sawRace {
		return ctx.errf("hedge requires a preceding race action in the same route")
	}
	if r.After.Duration <= 0 {
		return ctx.errf("hedge requires a positive after duration (migration from f7-09: parameterless hedge is no longer supported; add after=<delay>)")
	}
	target := strings.TrimSpace(r.Target)
	if target != "" && target == ctx.route {
		return ctx.errf("hedge must not target the route it belongs to (use a named subroute)")
	}
	ctx.plan.Hedge = HedgeConfig{
		After:    r.After.Duration,
		Target:   target,
		Internal: target == "",
	}
	return nil
}
