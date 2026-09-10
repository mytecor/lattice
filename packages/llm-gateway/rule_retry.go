package main

import (
	"strings"
	"time"
)

// RetryRule configures a bounded repeated transition to another named route:
// it owns only attempts, the backoff schedule and the target route. The retry
// condition (which terminal failures are retryable) lives entirely in the
// destination route's filter; retry no longer knows about error classes,
// providers, scope or batch sizes. The target's own filter decides whether the
// transition applies for the incoming failure.
type RetryRule struct {
	ruleBase
	Target   string         `json:"target"`
	Attempts int            `json:"attempts"`
	Backoff  *BackoffConfig `json:"backoff"`
}

// apply validates the retry schedule and normalizes backoff defaults (a
// constant 100ms..1s backoff) into the compiled route.
func (r *RetryRule) apply(ctx *stageContext) error {
	if !ctx.st.sawRace {
		return ctx.errf("retry requires a preceding race action in the same route")
	}
	target := strings.TrimSpace(r.Target)
	if target == "" {
		return ctx.errf("retry requires a non-empty target route")
	}
	if target == ctx.route {
		return ctx.errf("retry must not target the route it belongs to (use a named subroute)")
	}
	if r.Attempts < 1 {
		return ctx.errf("retry attempts must be at least 1")
	}
	retry := RetryConfig{Target: target, Attempts: r.Attempts}
	if r.Backoff != nil {
		retry.Backoff = *r.Backoff
	}
	if retry.Backoff.Initial.Duration == 0 {
		retry.Backoff.Initial.Duration = 100 * time.Millisecond
	}
	if retry.Backoff.Max.Duration == 0 {
		retry.Backoff.Max.Duration = time.Second
	}
	ctx.plan.Retry = retry
	return nil
}
