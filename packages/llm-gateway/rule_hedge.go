package main

import "strings"

// HedgeRule lets the named target route start after a fixed delay even while
// the current route's branches are still running: the hedge target is an
// ordinary route that describes its own filters, providers, mapping and race.
// The hedge is latency-only: it launches only while branches are still in
// flight, so a fast terminal failure is never delayed by the hedge window.
type HedgeRule struct {
	ruleBase
	After  Duration `json:"after"`
	Target string   `json:"target"`
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
	if target == "" {
		return ctx.errf("hedge requires a non-empty target route")
	}
	if target == ctx.route {
		return ctx.errf("hedge must not target the route it belongs to (use a named subroute)")
	}
	ctx.plan.Hedge = HedgeConfig{After: r.After.Duration, Target: target}
	return nil
}
