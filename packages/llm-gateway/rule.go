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
	model() string
	action() string
	apply(ctx *stageContext) error
	isRule()
}

// ruleBase carries the genuinely common routing-rule data: the logical model
// selector and the action identity. Action-specific fields never live here;
// they belong to the concrete types that embed ruleBase.
type ruleBase struct {
	Match struct {
		Model string `json:"model"`
	} `json:"match"`
	Action string `json:"action"`
}

func (b *ruleBase) model() string  { return strings.TrimSpace(b.Match.Model) }
func (b *ruleBase) action() string { return strings.ToLower(strings.TrimSpace(b.Action)) }
func (b *ruleBase) isRule()        {}

// setIdentity assigns the shared identity (logical model selector and action
// name). It exists for programmatic construction in tests and for exact
// legacy migrations at the decode boundary.
func (b *ruleBase) setIdentity(model, action string) {
	b.Match.Model = model
	b.Action = action
}

// Routing action names. The canonical pipeline order is map → rank → lease →
// affinity → race → retry → hedge → semaphore → timeout; fallback is the
// terminal route-creating action of the optional second stage. One or more
// consecutive map actions build the pending candidate pool of ready target
// pairs (provider ID, native model); rank transforms it and the
// route-creating action (race, or fallback for the second stage) keeps an
// immutable snapshot.
const (
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
	ActionMap:       {rank: 1, new: func() Rule { return &MapRule{} }},
	ActionRank:      {rank: 2, new: func() Rule { return &RankRule{} }},
	ActionLease:     {rank: 3, new: func() Rule { return &LeaseRule{} }},
	ActionAffinity:  {rank: 4, new: func() Rule { return &AffinityRule{} }},
	ActionRace:      {rank: 5, new: func() Rule { return &RaceRule{} }},
	ActionRetry:     {rank: 6, new: func() Rule { return &RetryRule{} }},
	ActionHedge:     {rank: 7, new: func() Rule { return &HedgeRule{} }},
	ActionSemaphore: {rank: 8, new: func() Rule { return &SemaphoreRule{} }},
	ActionTimeout:   {rank: 9, new: func() Rule { return &TimeoutRule{} }},
	ActionFallback:  {rank: 10, new: func() Rule { return &FallbackRule{} }},
}

// ruleErrf formats a routing-rule error that always carries the rule index,
// the logical model and the action followed by the concrete cause (never
// credentials or internal URLs).
func ruleErrf(index int, model, action, format string, args ...any) error {
	return fmt.Errorf("routing rule %d (model %q, action %q): %s",
		index, model, action, fmt.Sprintf(format, args...))
}
