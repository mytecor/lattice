package main

import "strings"

// MapRule binds the current provider selection (built by a preceding filter
// provider action) to one native model id and appends the ready target pairs
// (provider ID, native model) to the route's candidate pool. Map performs only
// native mapping: provider selection and filtering live exclusively in the
// preceding filter action.
type MapRule struct {
	ruleBase
	Native string `json:"native"`
}

// apply validates the native id and adds the current selection to the pending
// pool. A provider may appear in a route pool only once.
func (r *MapRule) apply(ctx *stageContext) error {
	if ctx.st.ranked {
		return ctx.errf("map must precede rank within a route")
	}
	if !ctx.st.hasSelection {
		return ctx.errf("map requires a preceding filter provider action (provider selection)")
	}
	if ctx.st.mapWithoutSelection {
		return ctx.errf("map requires a filter provider action after the previous map (provider selection)")
	}
	r.Native = strings.TrimSpace(r.Native)
	if r.Native == "" {
		return ctx.errf("map requires a non-empty native model id")
	}
	seen := make(map[string]struct{}, len(ctx.st.pending))
	for _, target := range ctx.st.pending {
		seen[target.Provider] = struct{}{}
	}
	for _, providerID := range sortedKeys(ctx.st.selection) {
		if _, duplicate := seen[providerID]; duplicate {
			return ctx.errf("provider %q appears more than once in the route candidate pool", providerID)
		}
		seen[providerID] = struct{}{}
		ctx.st.pending = append(ctx.st.pending, Target{Provider: providerID, Model: r.Native})
	}
	ctx.st.sawMap = true
	// The selection is consumed by this map: the next map must be preceded by
	// a fresh filter provider action.
	ctx.st.mapWithoutSelection = true
	return nil
}
