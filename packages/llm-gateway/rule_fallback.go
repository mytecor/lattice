package main

// FallbackRule is the terminal route-creating action of the optional second
// stage: an immutable target-pool snapshot (built by the preceding fallback
// stage maps) plus the error classes that transition from the primary stage
// and the dispatch mode. It carries no provider, native, lease, hedge or
// semaphore fields.
type FallbackRule struct {
	ruleBase
	On               []string `json:"on"`
	FallbackStrategy string   `json:"fallback_strategy"`
}

// apply validates the fallback stage and keeps an immutable snapshot of the
// fallback pool with the transition error filter and dispatch mode.
func (r *FallbackRule) apply(ctx *stageContext) error {
	if !ctx.st.primaryRace {
		return ctx.errf("fallback requires a preceding primary race stage")
	}
	if !ctx.st.inFallback || !ctx.st.sawMap {
		return ctx.errf("fallback requires a preceding map action that starts the fallback stage")
	}
	if ctx.plan.Fallback != nil || ctx.st.fallbackDeclared {
		return ctx.errf("fallback already declared for this model")
	}
	mode := r.FallbackStrategy
	if mode == "" {
		mode = "serial"
	}
	if mode != "serial" && mode != "race" && mode != "hedge" {
		return ctx.errf("unsupported fallback_strategy %q", mode)
	}
	ctx.plan.Fallback = &FallbackRoute{
		Pool: append([]Target(nil), ctx.st.pending...),
		Mode: mode,
		On:   parseErrorClasses(r.On),
	}
	ctx.st.fallbackDeclared = true
	return nil
}
