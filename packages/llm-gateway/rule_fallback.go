package main

import "strings"

// FallbackRule is an explicit one-shot transition to another named route,
// exactly like retry but without attempts/backoff: the current route's
// terminal failure transitions into the fallback target when the target's own
// filter admits it. There is no special second-stage compiler semantics; a
// fallback subroute is compiled by the same named-route compiler as every
// other route.
type FallbackRule struct {
	ruleBase
	Target string `json:"target"`
}

// apply validates the fallback target and records the transition.
func (r *FallbackRule) apply(ctx *stageContext) error {
	if !ctx.st.sawRace {
		return ctx.errf("fallback requires a preceding race action in the same route")
	}
	target := strings.TrimSpace(r.Target)
	if target == "" {
		return ctx.errf("fallback requires a non-empty target route")
	}
	if target == ctx.route {
		return ctx.errf("fallback must not target the route it belongs to (use a named subroute)")
	}
	ctx.plan.Fallback = FallbackConfig{Target: target}
	return nil
}
