package main

// LeaseRule configures the winner lease: the leased provider is promoted to
// the top of the ranking, the lease is renewed on success, and released on
// configured hard failures or after a number of consecutive slow starts.
type LeaseRule struct {
	ruleBase
	Source                 string   `json:"source"`
	Duration               Duration `json:"duration"`
	RenewOnSuccess         *bool    `json:"renew_on_success"`
	ReleaseOn              []string `json:"release_on"`
	ReleaseAfterSlowStarts int      `json:"release_after_slow_starts"`
	SlowStart              Duration `json:"slow_start"`
}

// apply validates the lease policy and normalizes it into the compiled plan.
// renew_on_success defaults to true when omitted.
func (r *LeaseRule) apply(ctx *stageContext) error {
	if !ctx.st.sawMap {
		return ctx.errf("lease requires a preceding map action")
	}
	if r.Source != "winner" {
		return ctx.errf("unsupported lease source %q (only \"winner\")", r.Source)
	}
	if r.Duration.Duration <= 0 {
		return ctx.errf("lease duration must be positive")
	}
	lease := LeaseConfig{Enabled: true, Source: "winner", Duration: r.Duration.Duration, RenewOnSuccess: true}
	if r.RenewOnSuccess != nil {
		lease.RenewOnSuccess = *r.RenewOnSuccess
	}
	lease.ReleaseOn = parseErrorClasses(r.ReleaseOn)
	if r.ReleaseAfterSlowStarts > 0 {
		if r.SlowStart.Duration <= 0 {
			return ctx.errf("release_after_slow_starts requires a positive slow_start")
		}
		lease.ReleaseAfterSlowStarts = r.ReleaseAfterSlowStarts
		lease.SlowStart = r.SlowStart.Duration
	}
	ctx.plan.Lease = lease
	return nil
}
