package main

// AffinityRule pins a successful Responses route to the provider that last
// served the conversation or response id, for the configured TTL. Unknown ids
// are ignored (on_missing) and pinned-provider failures stay fail-closed.
type AffinityRule struct {
	ruleBase
	Sources           []string `json:"sources"`
	TTL               Duration `json:"ttl"`
	OnMissing         string   `json:"on_missing"`
	OnProviderFailure string   `json:"on_provider_failure"`
}

// apply validates the affinity policy and normalizes it into the compiled
// plan.
func (r *AffinityRule) apply(ctx *stageContext) error {
	if !ctx.st.sawMap {
		return ctx.errf("affinity requires a preceding map action")
	}
	if len(r.Sources) == 0 {
		return ctx.errf("affinity requires at least one source")
	}
	for _, source := range r.Sources {
		switch source {
		case "responses.conversation", "responses.previous_response_id":
		default:
			return ctx.errf("unsupported affinity source %q", source)
		}
	}
	if r.TTL.Duration <= 0 {
		return ctx.errf("affinity ttl must be positive")
	}
	if r.OnMissing != "ignore" {
		return ctx.errf("unsupported on_missing %q (only \"ignore\")", r.OnMissing)
	}
	if r.OnProviderFailure != "fail-closed" {
		return ctx.errf("unsupported on_provider_failure %q (only \"fail-closed\")", r.OnProviderFailure)
	}
	ctx.plan.Affinity = AffinityConfig{
		Enabled: true, Sources: append([]string(nil), r.Sources...),
		TTL: r.TTL.Duration, OnMissing: "ignore", OnProviderFailure: "fail-closed",
	}
	return nil
}
