package main

import (
	"strings"
	"time"
)

// RetryRule configures repeated upstream attempts: either internal scheduler
// retry across available candidate targets/tiers (when target is omitted) or
// transition to another named route.
type RetryRule struct {
	ruleBase
	Target           string         `json:"target,omitempty"`
	Attempts         int            `json:"attempts,omitempty"`
	MaxAttempts      int            `json:"max_attempts,omitempty"`
	MaxAttemptsCamel int            `json:"maxAttempts,omitempty"`
	Backoff          *BackoffConfig `json:"backoff,omitempty"`
}

// apply validates the retry schedule and normalizes backoff defaults into the
// compiled route.
func (r *RetryRule) apply(ctx *stageContext) error {
	if !ctx.st.sawRace {
		return ctx.errf("retry requires a preceding race action in the same route")
	}
	target := strings.TrimSpace(r.Target)
	if target != "" && target == ctx.route {
		return ctx.errf("retry must not target the route it belongs to (use a named subroute)")
	}
	if target != "" && r.Attempts < 1 {
		return ctx.errf("retry attempts must be at least 1")
	}
	attempts := r.Attempts
	if attempts == 0 && r.MaxAttempts != 0 {
		attempts = r.MaxAttempts
	}
	if attempts == 0 && r.MaxAttemptsCamel != 0 {
		attempts = r.MaxAttemptsCamel
	}
	if attempts < 1 {
		attempts = 3
	}
	retry := RetryConfig{
		Target:   target,
		Attempts: attempts,
		Internal: target == "",
	}
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
