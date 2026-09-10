package main

import (
	"fmt"
	"strings"
)

// Rule is the external routing-rule contract. Every concrete action owns only
// its own fields, validates itself and applies itself to the compiler state
// through apply. The unexported methods keep the implementation set closed
// inside this package: no single public struct carries the union of all
// action fields, and adding an action touches only ruleRegistry and one new
// implementation file.
type Rule interface {
	route() string
	action() string
	apply(ctx *stageContext) error
	isRule()
}

// ruleBase carries the genuinely common routing-rule data: the named route
// the rule belongs to and the action identity. Action-specific fields never
// live here; they belong to the concrete types that embed ruleBase. The route
// is a plain string naming a routing scope ("standard", "standard.retry",
// "standard.fallback"); dots are a naming convention and carry no runtime
// meaning.
type ruleBase struct {
	Route  string `json:"route"`
	Action string `json:"action"`
}

func (b *ruleBase) route() string  { return strings.TrimSpace(b.Route) }
func (b *ruleBase) action() string { return strings.ToLower(strings.TrimSpace(b.Action)) }
func (b *ruleBase) isRule()        {}

// setIdentity assigns the shared identity (route name and action name). It
// exists for programmatic construction in tests.
func (b *ruleBase) setIdentity(route, action string) {
	b.Route = route
	b.Action = action
}

// Routing action names. A route is a flat, ordered sequence of these actions:
// filter (selection/applicability), map (native mapping), rank, lease,
// affinity, race (execution), retry/fallback/hedge (explicit transitions to
// other named routes), semaphore and timeout (request-wide safety).
const (
	ActionFilter    = "filter"
	ActionMap       = "map"
	ActionRank      = "rank"
	ActionLease     = "lease"
	ActionAffinity  = "affinity"
	ActionRace      = "race"
	ActionRetry     = "retry"
	ActionHedge     = "hedge"
	ActionSemaphore = "semaphore"
	ActionTimeout   = "timeout"
	ActionFallback  = "fallback"
)

// ruleDescriptor declares one routing action: its canonical pipeline position
// and a factory used by the strict decoder. Adding a new action changes only
// this registry plus the new implementation file; no central switch is
// extended.
type ruleDescriptor struct {
	rank int
	new  func() Rule
}

// ruleRegistry is the action discriminator shared by the decoder and the
// compiler: an unknown action fails at the registry boundary before any
// concrete type is constructed.
var ruleRegistry = map[string]ruleDescriptor{
	ActionFilter:    {rank: 1, new: func() Rule { return &FilterRule{} }},
	ActionMap:       {rank: 2, new: func() Rule { return &MapRule{} }},
	ActionRank:      {rank: 3, new: func() Rule { return &RankRule{} }},
	ActionLease:     {rank: 4, new: func() Rule { return &LeaseRule{} }},
	ActionAffinity:  {rank: 5, new: func() Rule { return &AffinityRule{} }},
	ActionRace:      {rank: 6, new: func() Rule { return &RaceRule{} }},
	ActionRetry:     {rank: 7, new: func() Rule { return &RetryRule{} }},
	ActionHedge:     {rank: 8, new: func() Rule { return &HedgeRule{} }},
	ActionSemaphore: {rank: 9, new: func() Rule { return &SemaphoreRule{} }},
	ActionTimeout:   {rank: 10, new: func() Rule { return &TimeoutRule{} }},
	ActionFallback:  {rank: 11, new: func() Rule { return &FallbackRule{} }},
}

// ruleErrf formats a routing-rule error that always carries the rule index,
// the named route and the action followed by the concrete cause (never
// credentials or internal URLs).
func ruleErrf(index int, route, action, format string, args ...any) error {
	return fmt.Errorf("routing rule %d (route %q, action %q): %s",
		index, route, action, fmt.Sprintf(format, args...))
}
