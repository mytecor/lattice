package main

import (
	"fmt"
	"sort"
	"strings"
)

// stageState tracks the per-route compilation of the provider selection and
// the pending candidate pool. Each named route owns one stageState; rules of
// the same route execute in order of appearance in the global routing_rules.
type stageState struct {
	providerUniverse    map[string]bool // providers referenced by the route's provider filters
	selection           map[string]bool // current provider selection built by the last provider filter
	hasSelection        bool
	mapWithoutSelection bool // a map consumed the selection; the next map needs a fresh filter
	pending             []Target
	sawMap              bool
	ranked              bool
	sawRace             bool
	modelEq             string
	entrySet            bool
	errorIn             map[ErrorClass]bool
	errorSet            bool
	attemptLT           int
	attemptSet          bool
	providerUnused      bool
}

// stageContext is the explicit compiler/builder state handed to every rule's
// apply method. It carries the shared per-rule identity (index, route, action,
// canonical rank), the provider registry and the per-route stage and compiled
// route being built. Concrete rules read and mutate only what they own.
type stageContext struct {
	index     int
	route     string
	action    string
	rank      int
	providers map[string]Provider
	st        *stageState
	plan      *compiledRoute
}

func (ctx *stageContext) errf(format string, args ...any) error {
	return ruleErrf(ctx.index, ctx.route, ctx.action, format, args...)
}

func (ctx *stageContext) applyModelFilter(model string) error {
	if model == "" {
		return ctx.errf("filter where.model requires a non-empty model id")
	}
	if ctx.st.entrySet {
		return ctx.errf("route %q already has an entry model filter", ctx.route)
	}
	ctx.st.entrySet = true
	ctx.st.modelEq = model
	return nil
}

func (ctx *stageContext) applyErrorFilter(classes []string) error {
	if len(classes) == 0 {
		return ctx.errf("filter where.error requires a non-empty in list")
	}
	if ctx.st.errorSet {
		return ctx.errf("route %q already has an error filter", ctx.route)
	}
	parsed := make(map[ErrorClass]bool, len(classes))
	for _, class := range classes {
		key := ErrorClass(strings.ToLower(strings.TrimSpace(class)))
		switch key {
		case ErrorTimeout, ErrorConnection, ErrorRateLimit, ErrorNotFound,
			ErrorModelNotFound, ErrorUpstream, ErrorInvalid, ErrorCancelled:
			parsed[key] = true
		default:
			return ctx.errf("filter where.error references unknown error class %q", class)
		}
	}
	ctx.st.errorSet = true
	ctx.st.errorIn = parsed
	return nil
}

func (ctx *stageContext) applyAttemptFilter(lt int) error {
	if lt < 1 {
		return ctx.errf("filter where.attempt requires lt >= 1")
	}
	if ctx.st.attemptSet {
		return ctx.errf("route %q already has an attempt filter", ctx.route)
	}
	ctx.st.attemptSet = true
	ctx.st.attemptLT = lt
	return nil
}

// applyProviderFilter builds a fresh provider selection from the route
// provider universe: an in list narrows the universe, a not_in list removes
// providers from the selection, and the unused flag marks the compiled pool as
// restricted to providers not yet used by the request. The first filter never
// permanently removes the other providers from the route: every subsequent
// provider filter selects again from the full universe, so different provider
// groups can map to different native models through filter+map sequences.
func (ctx *stageContext) applyProviderFilter(cond *filterProviderCond) error {
	inValues := cond.inValues()
	if cond.hasIn() && len(inValues) == 0 {
		return ctx.errf("filter where.provider requires a non-empty in list when in is declared")
	}
	if !cond.hasIn() && len(cond.NotIn) == 0 && !cond.Unused {
		return ctx.errf("filter where.provider requires at least one of in, not_in or unused")
	}
	for _, provider := range append(append([]string(nil), inValues...), cond.NotIn...) {
		provider = strings.TrimSpace(provider)
		if _, exists := ctx.providers[provider]; !exists {
			return ctx.errf("filter references unknown provider %q", provider)
		}
	}
	if cond.Unused {
		ctx.st.providerUnused = true
	}
	ctx.st.selection = make(map[string]bool)
	selected := make([]string, 0, len(ctx.st.providerUniverse))
	for provider := range ctx.st.providerUniverse {
		selected = append(selected, provider)
	}
	// in narrows the universe, not_in further removes providers.
	if cond.hasIn() {
		in := make(map[string]bool, len(inValues))
		for _, provider := range inValues {
			in[strings.TrimSpace(provider)] = true
		}
		for _, provider := range selected {
			if !in[provider] {
				delete(ctx.st.selection, provider)
				continue
			}
			ctx.st.selection[provider] = true
		}
	}
	notIn := make(map[string]bool, len(cond.NotIn))
	for _, provider := range cond.NotIn {
		notIn[strings.TrimSpace(provider)] = true
	}
	for provider := range ctx.st.selection {
		if notIn[provider] {
			delete(ctx.st.selection, provider)
		}
	}
	if !cond.hasIn() {
		// Pure not_in selection: start from the whole universe.
		for provider := range ctx.st.providerUniverse {
			if !notIn[provider] {
				ctx.st.selection[provider] = true
			}
		}
	}
	// An empty in list must not silently select nothing.
	if !cond.hasIn() && len(ctx.st.selection) == 0 && len(ctx.st.providerUniverse) == 0 {
		return ctx.errf("filter where.provider selects nothing: no provider universe is known for route %q", ctx.route)
	}
	ctx.st.hasSelection = true
	ctx.st.mapWithoutSelection = false
	return nil
}

// routeCompileResult is the compiled routing graph plus the derived entry-model
// registry used by discovery and request routing.
type routeCompileResult struct {
	routes  map[string]*compiledRoute
	models  []string
	entries map[string]*compiledRoute // logical model → entry route
}

// compileRules compiles the flat routing rule list into the immutable named
// route graph. Rules are grouped by route preserving the order of first
// appearance; within a route they execute in order of appearance. Each rule
// applies itself through typed dispatch, so there is no central switch
// carrying the semantics of all actions. Target routes are resolved and the
// whole graph (missing targets, orphan routes, duplicate entry models,
// transition cycles) validated here, at compile stage.
func compileRoutes(rules []Rule, providers map[string]Provider) (*routeCompileResult, error) {
	order := make([]string, 0, len(rules))
	groups := make(map[string][]Rule)
	indices := make(map[string][]int)
	for index, rule := range rules {
		route := rule.route()
		if route == "" {
			return nil, fmt.Errorf("routing rule %d has no route", index)
		}
		action := rule.action()
		if action == "" {
			return nil, fmt.Errorf("routing rule %d (route %q) has no action", index, route)
		}
		if _, ok := ruleRegistry[action]; !ok {
			return nil, fmt.Errorf("routing rule %d (route %q) has unsupported action %q", index, route, action)
		}
		if _, exists := groups[route]; !exists {
			order = append(order, route)
		}
		groups[route] = append(groups[route], rule)
		indices[route] = append(indices[route], index)
	}

	routes := make(map[string]*compiledRoute, len(order))
	states := make(map[string]*stageState, len(order))
	for _, name := range order {
		st := &stageState{
			providerUniverse: make(map[string]bool),
			selection:        make(map[string]bool),
		}
		states[name] = st
		// Pass A: collect the provider universe from every provider filter of
		// the route, so a later filter can meaningfully select from the full
		// set (not_in in particular needs to know which providers exist).
		for _, rule := range groups[name] {
			if filter, ok := rule.(*FilterRule); ok && filter.appliesToProvider() {
				for _, provider := range append(append([]string(nil), filter.Where.Provider.inValues()...), filter.Where.Provider.NotIn...) {
					if trimmed := strings.TrimSpace(provider); trimmed != "" {
						st.providerUniverse[trimmed] = true
					}
				}
			}
		}
		plan := &compiledRoute{Name: name, Pool: []Target{}}
		for offset, rule := range groups[name] {
			action := rule.action()
			ctx := &stageContext{
				index:     indices[name][offset],
				route:     name,
				action:    action,
				rank:      ruleRegistry[action].rank,
				providers: providers,
				st:        st,
				plan:      plan,
			}
			if err := rule.apply(ctx); err != nil {
				return nil, err
			}
		}
		// Finalize the per-route applicability flags.
		if !st.sawRace {
			return nil, fmt.Errorf("route %q has no route-creating race action", name)
		}
		plan.Entry = st.entrySet
		plan.ModelEq = st.modelEq
		if st.errorSet {
			plan.ErrorIn = st.errorIn
		}
		plan.AttemptLT = st.attemptLT
		plan.ProviderUnused = st.providerUnused
		routes[name] = plan
	}

	// Copy the pending pool into the immutable race snapshot (already done by
	// the race action) and validate the graph.
	if err := resolveRouteGraph(routes); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(routes))
	entries := make(map[string]*compiledRoute, len(routes))
	seenModels := make(map[string]string)
	for _, name := range order {
		plan := routes[name]
		if !plan.Entry {
			continue
		}
		if owner, duplicate := seenModels[plan.ModelEq]; duplicate {
			return nil, fmt.Errorf("duplicate entry route: %q and %q both declare model %q", owner, name, plan.ModelEq)
		}
		seenModels[plan.ModelEq] = name
		models = append(models, plan.ModelEq)
		entries[plan.ModelEq] = plan
	}
	sort.Strings(models)
	return &routeCompileResult{routes: routes, models: models, entries: entries}, nil
}

// resolveRouteGraph validates the compiled route graph: every transition
// target must exist, non-entry routes must be referenced by at least one
// transition, semaphore/timeout are request-wide and may live only on entry
// routes, and the transition graph must be acyclic.
func resolveRouteGraph(routes map[string]*compiledRoute) error {
	for name, route := range routes {
		if route.Retry.Target != "" {
			target, ok := routes[route.Retry.Target]
			if !ok {
				return fmt.Errorf("route %q retry target %q does not exist", name, route.Retry.Target)
			}
			route.retryTarget = target
		}
		if route.Fallback.Target != "" {
			target, ok := routes[route.Fallback.Target]
			if !ok {
				return fmt.Errorf("route %q fallback target %q does not exist", name, route.Fallback.Target)
			}
			route.fallbackTarget = target
		}
		if route.Hedge.Target != "" {
			target, ok := routes[route.Hedge.Target]
			if !ok {
				return fmt.Errorf("route %q hedge target %q does not exist", name, route.Hedge.Target)
			}
			route.hedgeTarget = target
		}
	}
	referenced := make(map[string]bool, len(routes))
	for _, route := range routes {
		if route.retryTarget != nil {
			referenced[route.retryTarget.Name] = true
		}
		if route.fallbackTarget != nil {
			referenced[route.fallbackTarget.Name] = true
		}
		if route.hedgeTarget != nil {
			referenced[route.hedgeTarget.Name] = true
		}
	}
	for name, route := range routes {
		if !route.Entry && !referenced[name] {
			return fmt.Errorf("route %q is not an entry route and is not referenced by any transition (either add a filter where.model or reference it from a retry/fallback/hedge target)", name)
		}
		if !route.Entry && (route.Semaphore.MaxCalls != 0 || route.RouteTimeout != 0) {
			return fmt.Errorf("route %q is a subroute and must not set semaphore/timeout: both are request-wide and live on the entry route", name)
		}
	}
	// Cycle detection over the transition graph: self-transitions are already
	// rejected by the retry/fallback/hedge actions, but indirect cycles
	// (a → retry b, b → fallback a) must also fail at compile stage.
	state := make(map[string]int, len(routes))
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("routing cycle detected involving route %q", name)
		case 2:
			return nil
		}
		state[name] = 1
		route := routes[name]
		for _, target := range []*compiledRoute{route.retryTarget, route.fallbackTarget, route.hedgeTarget} {
			if target != nil {
				if err := visit(target.Name); err != nil {
					return err
				}
			}
		}
		state[name] = 2
		return nil
	}
	for name := range routes {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}
