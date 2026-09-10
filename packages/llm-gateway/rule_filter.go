package main

import "strings"

// FilterRule is the typed routing primitive with the single responsibility of
// restricting the applicability of a route or the current provider selection.
// It never performs mapping, execution or control flow. A filter declares
// exactly one condition dimension:
//
//   - model: {"eq": "standard"} makes the route an entry route for the logical
//     model "standard" (request-level applicability);
//   - provider: {"in": [...], "not_in": [...], "unused": true} builds a fresh
//     provider selection from the route provider universe;
//   - error: {"in": ["429", "5xx", ...]} gates a transition into this route on
//     the class of the incoming terminal failure (destination owns
//     applicability);
//   - attempt: {"lt": N} gates entry into this route on the current attempt
//     number (bounds repeated transitions).
//
// Every dimension is validated against the existing typed failure classes and
// the provider registry; there is no substring matching and no generic
// expression language.
type FilterRule struct {
	ruleBase
	Where FilterWhere `json:"where"`
}

// FilterWhere carries the optional condition dimensions. Exactly one of the
// pointers must be set; DisallowUnknownFields on FilterRule rejects unknown
// operators and unknown dimensions at the decode boundary.
type FilterWhere struct {
	Model    *filterModelCond    `json:"model"`
	Provider *filterProviderCond `json:"provider"`
	Error    *filterErrorCond    `json:"error"`
	Attempt  *filterAttemptCond  `json:"attempt"`
}

// filterModelCond is the request-level entry filter: route applies to requests
// whose logical model equals Eq.
type filterModelCond struct {
	Eq string `json:"eq"`
}

// filterProviderCond builds a provider selection from the route provider
// universe: In selects a subset, NotIn removes providers from that selection,
// and Unused restricts the compiled pool to providers not yet used by the
// current request graph (explicit routing policy, never a hidden retry
// property).
type filterProviderCond struct {
	// In is a pointer so strict validation can distinguish an omitted selector
	// from an explicitly empty allowlist. The latter is always a configuration
	// error instead of silently expanding to the route provider universe.
	In     *[]string `json:"in"`
	NotIn  []string  `json:"not_in"`
	Unused bool      `json:"unused"`
}

func (c *filterProviderCond) hasIn() bool { return c != nil && c.In != nil }

func (c *filterProviderCond) inValues() []string {
	if !c.hasIn() {
		return nil
	}
	return *c.In
}

// filterErrorCond gates a transition into the route on the incoming failure:
// the route applies only when the terminal failure class is one of In.
type filterErrorCond struct {
	In []string `json:"in"`
}

// filterAttemptCond gates entry into the route on the current attempt number:
// the route applies only when the attempt is less than Lt.
type filterAttemptCond struct {
	Lt int `json:"lt"`
}

// appliesToModel reports whether the filter carries a request-level model
// condition.
func (f *FilterRule) appliesToModel() bool { return f.Where.Model != nil }

// appliesToError reports whether the filter carries an error-class condition.
func (f *FilterRule) appliesToError() bool { return f.Where.Error != nil }

// appliesToProvider reports whether the filter carries a provider-selection
// condition.
func (f *FilterRule) appliesToProvider() bool { return f.Where.Provider != nil }

// appliesToAttempt reports whether the filter carries an attempt condition.
func (f *FilterRule) appliesToAttempt() bool { return f.Where.Attempt != nil }

// apply validates the single active dimension and applies it to the per-route
// builder state: model/error/attempt conditions land on the route
// applicability, provider conditions build a fresh provider selection.
func (r *FilterRule) apply(ctx *stageContext) error {
	where := r.Where
	dimensions := 0
	for _, set := range []bool{r.appliesToModel(), r.appliesToProvider(), r.appliesToError(), r.appliesToAttempt()} {
		if set {
			dimensions++
		}
	}
	if dimensions != 1 {
		return ctx.errf("filter must declare exactly one of model, provider, error or attempt")
	}
	switch {
	case r.appliesToModel():
		return ctx.applyModelFilter(strings.TrimSpace(where.Model.Eq))
	case r.appliesToError():
		return ctx.applyErrorFilter(where.Error.In)
	case r.appliesToAttempt():
		return ctx.applyAttemptFilter(where.Attempt.Lt)
	default:
		return ctx.applyProviderFilter(where.Provider)
	}
}
