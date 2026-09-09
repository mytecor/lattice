package main

import "time"

// RetryRule configures the bounded retry schedule after the initial race:
// scope "same" repeats the original selection, scope "next" uses the next
// unused ranked targets and never repeats a used provider. Attempts, the
// error filter and the constant/exponential backoff are normalized here.
type RetryRule struct {
	ruleBase
	Count    int            `json:"count"`
	Scope    string         `json:"scope"`
	Attempts int            `json:"attempts"`
	On       []string       `json:"on"`
	Backoff  *BackoffConfig `json:"backoff"`
}

// apply validates the retry schedule and normalizes defaults (scope "same", a
// constant 100ms..1s backoff) into the compiled plan.
func (r *RetryRule) apply(ctx *stageContext) error {
	if !ctx.st.primaryRace {
		return ctx.errf("retry requires a preceding race action")
	}
	scope := r.Scope
	if scope == "" {
		scope = "same"
	}
	if scope != "same" && scope != "next" {
		return ctx.errf("unsupported retry scope %q (only \"same\" and \"next\")", scope)
	}
	if r.Attempts < 1 {
		return ctx.errf("retry attempts must be at least 1")
	}
	retry := RetryConfig{Scope: scope, Attempts: r.Attempts, On: parseErrorClasses(r.On)}
	if scope == "next" && r.Count < 1 {
		return ctx.errf("retry scope \"next\" requires a positive count (batch size)")
	}
	retry.Count = r.Count
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
