package main

import "fmt"

// stageState tracks the per-model compilation of the pending candidate pool
// across the primary and optional fallback stages.
type stageState struct {
	pending          []Target
	sawMap           bool
	ranked           bool
	primaryRace      bool
	inFallback       bool
	fallbackDeclared bool
	lastRank         int
}

func (st *stageState) beginFallbackStage() {
	st.pending = nil
	st.sawMap = false
	st.ranked = false
	st.inFallback = true
	st.lastRank = 1
}

// stageContext is the explicit compiler/builder state handed to every rule's
// apply method. It carries the shared per-rule identity (index, model, action,
// canonical rank), the provider registry and the per-model stage and plan
// being built. Concrete rules read and mutate only what they own.
type stageContext struct {
	index     int
	model     string
	action    string
	rank      int
	providers map[string]Provider
	st        *stageState
	plan      *Plan
}

func (ctx *stageContext) errf(format string, args ...any) error {
	return ruleErrf(ctx.index, ctx.model, ctx.action, format, args...)
}

// advance performs the generic stage/order bookkeeping shared by every rule:
// a map after the primary race opens the optional fallback stage; otherwise
// the canonical rank must not move backwards within a stage, and the fallback
// stage admits only map, rank and fallback.
func (ctx *stageContext) advance() error {
	st, action, rank := ctx.st, ctx.action, ctx.rank
	switch {
	case action == ActionMap && st.primaryRace && !st.fallbackDeclared && !st.inFallback:
		st.beginFallbackStage()
	case !st.inFallback && rank < st.lastRank:
		return ctx.errf("actions out of pipeline order: %q must not follow an action at position %d", action, st.lastRank)
	case st.inFallback && action != ActionMap && action != ActionRank && action != ActionFallback:
		return ctx.errf("only map, rank and fallback are allowed in the fallback stage")
	}
	return nil
}

// compilePlans validates the flat pipeline and produces the compiled per-model
// Plan through typed dispatch: each rule applies its own validation and
// mutation to the stage/plan it owns, and there is no central switch carrying
// the semantics of all actions. Errors always carry the rule index, model,
// action and the concrete cause. One or more consecutive map actions form the
// pending candidate pool; rank transforms it, and the route-creating action
// (race for the primary stage, fallback for the optional second stage) keeps
// an immutable snapshot. A provider may appear in a pending pool only once; a
// new map set after race belongs to the fallback stage and never changes the
// compiled primary stage.
func compilePlans(rules []Rule, providers map[string]Provider) (map[string]Plan, error) {
	plans := make(map[string]Plan)
	states := make(map[string]*stageState)
	for index, rule := range rules {
		model := rule.model()
		if model == "" {
			return nil, fmt.Errorf("routing rule %d has no match.model", index)
		}
		action := rule.action()
		if action == "" {
			return nil, fmt.Errorf("routing rule %d (model %q) has no action", index, model)
		}
		descriptor, ok := ruleRegistry[action]
		if !ok {
			return nil, fmt.Errorf("routing rule %d (model %q) has unsupported action %q", index, model, action)
		}
		st := states[model]
		if st == nil {
			st = &stageState{}
			states[model] = st
		}
		plan := plans[model]
		plan.LogicalModel = model
		ctx := &stageContext{
			index:     index,
			model:     model,
			action:    action,
			rank:      descriptor.rank,
			providers: providers,
			st:        st,
			plan:      &plan,
		}
		if err := ctx.advance(); err != nil {
			return nil, err
		}
		if err := rule.apply(ctx); err != nil {
			return nil, err
		}
		st.lastRank = descriptor.rank
		plans[model] = *ctx.plan
	}

	for model, plan := range plans {
		if len(plan.Pool) == 0 {
			return nil, fmt.Errorf("logical model %q has no route-creating race action", model)
		}
		if !states[model].primaryRace {
			return nil, fmt.Errorf("logical model %q has no race action", model)
		}
		if states[model].inFallback && !states[model].fallbackDeclared {
			return nil, fmt.Errorf("logical model %q has a dangling map without a route-creating fallback action", model)
		}
		if plan.LogicalModel == "" {
			plan.LogicalModel = model
		}
		plans[model] = plan
	}
	return plans, nil
}
