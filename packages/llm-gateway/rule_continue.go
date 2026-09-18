package main

import "time"

// ContinueRule declares the in-gateway stream takeover policy for a route.
// It is a request-wide policy declared on an entry route: when the relayed
// winner stream produces meaningful content/reasoning and then either idles
// past Idle or closes without a finish_reason, the gateway continues the same
// client stream by re-dispatching the request (with the partial output
// reshared per Reshare) to another provider, instead of surfacing an error to
// the client. It affects only streaming chat requests at runtime; the rule
// itself is inert for non-streaming requests.
type ContinueRule struct {
	ruleBase
	// Idle is the stall threshold for the relayed winner stream (replaces the
	// route-wide stream idle timeout while the policy is active). Required and
	// must be positive.
	Idle Duration `json:"idle"`
	// Reshare is the partial-output handoff mode: "full" (default) appends
	// every relayed reasoning/content delta as assistant context before
	// re-dispatching; any other value is rejected.
	Reshare string `json:"reshare,omitempty"`
}

// apply validates the takeover policy and stores it in the compiled entry
// route.
func (r *ContinueRule) apply(ctx *stageContext) error {
	if !ctx.st.sawRace {
		return ctx.errf("continue requires a preceding race action")
	}
	if !ctx.st.entrySet {
		return ctx.errf("continue must be declared on an entry route (the route needs a filter where.model) so the logical model and its relay policy are known")
	}
	if r.Idle.Duration <= 0 {
		return ctx.errf("continue idle must be a positive duration")
	}
	if r.Idle.Duration < 5*time.Second {
		return ctx.errf("continue idle must be at least 5s to avoid taking over genuinely slow reasoning streams")
	}
	reshare := r.Reshare
	if reshare == "" {
		reshare = "full"
	}
	if reshare != "full" {
		return ctx.errf("continue reshare supports only \"full\", got %q", reshare)
	}
	ctx.plan.Continue = ContinueConfig{
		Enabled: true,
		Idle:    r.Idle.Duration,
		Reshare: reshare,
	}
	return nil
}
