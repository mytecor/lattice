package main

import "sort"

// RankRule orders the route pending candidate pool by provider priority
// (descending), keeping the existing relative order on ties.
type RankRule struct {
	ruleBase
	Strategy string `json:"strategy"`
}

// apply validates the ranking strategy and stably sorts the pending pool.
func (r *RankRule) apply(ctx *stageContext) error {
	if ctx.st.sawRace {
		return ctx.errf("rank must precede the race action within a route")
	}
	if !ctx.st.sawMap {
		return ctx.errf("rank requires a preceding map action")
	}
	if ctx.st.ranked {
		return ctx.errf("rank already declared for this route")
	}
	if r.Strategy != "priority" {
		return ctx.errf("unsupported rank strategy %q (only \"priority\" is implemented)", r.Strategy)
	}
	sort.SliceStable(ctx.st.pending, func(i, j int) bool {
		left, right := ctx.providers[ctx.st.pending[i].Provider], ctx.providers[ctx.st.pending[j].Provider]
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		return ctx.st.pending[i].Provider < ctx.st.pending[j].Provider
	})
	ctx.st.ranked = true
	return nil
}
