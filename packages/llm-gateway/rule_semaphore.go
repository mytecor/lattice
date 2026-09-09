package main

// SemaphoreRule sets the per-request safety bounds shared by the primary and
// fallback routes: total calls, simultaneously executing calls and calls to
// one provider.
type SemaphoreRule struct {
	ruleBase
	MaxCalls            int `json:"max_calls"`
	MaxInFlight         int `json:"max_in_flight"`
	MaxCallsPerProvider int `json:"max_calls_per_provider"`
}

// apply validates that every bound is positive and stores them in the
// compiled plan.
func (r *SemaphoreRule) apply(ctx *stageContext) error {
	if !ctx.st.primaryRace {
		return ctx.errf("semaphore requires a preceding race action")
	}
	if r.MaxCalls < 1 || r.MaxInFlight < 1 || r.MaxCallsPerProvider < 1 {
		return ctx.errf("max_calls, max_in_flight and max_calls_per_provider must all be positive")
	}
	ctx.plan.Semaphore = SemaphoreConfig{
		MaxCalls: r.MaxCalls, MaxInFlight: r.MaxInFlight, MaxCallsPerProvider: r.MaxCallsPerProvider,
	}
	return nil
}
