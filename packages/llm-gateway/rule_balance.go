package main

import (
	"strings"
	"time"
)

// BalanceRule is the runtime provider-balance action. It gives the scheduler
// a runtime step that chooses which provider the route starts with before the
// race — the missing piece that makes distribution actually possible, because
// a pure first-responder race always favours the fastest provider regardless
// of priority. The action is compile-time typed like lease and applied at
// runtime in the scheduler (applyBalance), and must live on the route before
// the race action. It owns no execution and never replaces race: it only
// controls the choice and the order of the race batch.
//
// The strategies differ only in the runtime selection policy:
//
//		p2c          — power of two choices: two random healthy candidates are
//	  drawn and the one with fewer in-flight branches wins. Spreads load under
//	  concurrency without latency feedback (f7-13 showed latency-weighted
//	  selection re-concentrates on the fastest provider);
//		round_robin — a per-route cursor rotates over the healthy candidates
//		  (maximum distribution; unhealthy providers are excluded by the floor);
//		adaptive    — weighted-random by static weight × health(p); spreads load
//		  and shifts it toward whoever is currently coping best;
//		weighted    — only the static weights, no health history.
//
// balance and lease are mutually exclusive on one route: both change the
// runtime choice/order, and a lease would silently override the balancing
// with winner-stickiness. The conflict fails at compile stage.
type BalanceRule struct {
	ruleBase
	Strategy    string         `json:"strategy"`
	Weights     map[string]int `json:"weights,omitempty"`
	Window      Duration       `json:"window"`
	ErrorBudget float64        `json:"error_budget"`
}

// apply validates the balance policy and normalizes it into the compiled
// route. It must come after the map/rank sequence (the pool exists) and before
// the race action, and must not share a route with lease.
func (r *BalanceRule) apply(ctx *stageContext) error {
	if ctx.st.sawRace {
		return ctx.errf("balance must precede the race action within a route")
	}
	if !ctx.st.sawMap {
		return ctx.errf("balance requires a preceding map action")
	}
	if ctx.st.sawBalance {
		return ctx.errf("balance already declared for this route")
	}
	if ctx.plan.Lease.Enabled {
		return ctx.errf("balance and lease are mutually exclusive on one route")
	}
	switch r.Strategy {
	case "p2c", "round_robin", "adaptive", "weighted":
	default:
		return ctx.errf("unsupported balance strategy %q (only \"p2c\", \"round_robin\", \"adaptive\" or \"weighted\")", r.Strategy)
	}
	weights := make(map[string]int, len(r.Weights))
	for provider, weight := range r.Weights {
		provider = strings.TrimSpace(provider)
		if _, exists := ctx.providers[provider]; !exists {
			return ctx.errf("balance weights reference unknown provider %q", provider)
		}
		if weight < 1 {
			return ctx.errf("balance weight for %q must be at least 1", provider)
		}
		weights[provider] = weight
	}
	window := r.Window.Duration
	if window <= 0 {
		window = 5 * time.Minute
	}
	errorBudget := r.ErrorBudget
	if errorBudget <= 0 {
		errorBudget = 0.2
	}
	if errorBudget > 1 {
		errorBudget = 1
	}
	ctx.plan.Balance = BalanceConfig{
		Enabled: true, Strategy: r.Strategy, Weights: weights,
		Window: window, ErrorBudget: errorBudget,
	}
	ctx.st.sawBalance = true
	return nil
}
