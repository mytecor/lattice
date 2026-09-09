package main

import "strings"

// MapRule binds one provider-native model id to an explicit set of provider
// IDs. One or more consecutive map actions build the pending candidate pool of
// ready target pairs (provider ID, native model) for the current stage; a map
// after the primary race starts the optional fallback stage.
type MapRule struct {
	ruleBase
	Native    string   `json:"native"`
	Providers []string `json:"providers"`
}

// apply validates the native id and provider list, normalizes the native id
// and appends the mapped target pairs to the pending pool. A provider may
// appear in a pending pool only once.
func (r *MapRule) apply(ctx *stageContext) error {
	if ctx.st.fallbackDeclared {
		return ctx.errf("map is not allowed after a declared fallback stage")
	}
	if ctx.st.ranked {
		return ctx.errf("map must precede rank within a stage")
	}
	r.Native = strings.TrimSpace(r.Native)
	if r.Native == "" {
		return ctx.errf("map requires a non-empty native model id")
	}
	if len(r.Providers) == 0 {
		return ctx.errf("map requires at least one provider id")
	}
	seen := make(map[string]struct{}, len(ctx.st.pending))
	for _, target := range ctx.st.pending {
		seen[target.Provider] = struct{}{}
	}
	for _, providerID := range r.Providers {
		providerID = strings.TrimSpace(providerID)
		if _, exists := ctx.providers[providerID]; !exists {
			return ctx.errf("map references unknown provider %q", providerID)
		}
		if _, duplicate := seen[providerID]; duplicate {
			return ctx.errf("provider %q appears more than once in the pending pool", providerID)
		}
		seen[providerID] = struct{}{}
		ctx.st.pending = append(ctx.st.pending, Target{Provider: providerID, Model: r.Native})
	}
	ctx.st.sawMap = true
	return nil
}
