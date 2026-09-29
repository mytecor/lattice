package main

import "strings"

// MapRule binds the current provider selection (built by a preceding filter
// provider action or declared directly in providers) to one native model id and
// appends the ready target pairs (provider ID, native model, tier) to the route's
// candidate pool.
type MapRule struct {
	ruleBase
	Native      string   `json:"native,omitempty"`
	NativeModel string   `json:"native_model,omitempty"`
	NativeCamel string   `json:"nativeModel,omitempty"`
	Providers   []string `json:"providers,omitempty"`
	Tier        int      `json:"tier,omitempty"`
}

// apply validates the native id and adds the candidates to the pending pool.
func (r *MapRule) apply(ctx *stageContext) error {
	if ctx.st.ranked {
		return ctx.errf("map must precede rank within a route")
	}
	model := strings.TrimSpace(r.Native)
	if model == "" {
		model = strings.TrimSpace(r.NativeModel)
	}
	if model == "" {
		model = strings.TrimSpace(r.NativeCamel)
	}
	if model == "" {
		return ctx.errf("map requires a non-empty native model id")
	}
	if r.Tier < 0 {
		return ctx.errf("map tier must not be negative")
	}
	r.Native = model

	if len(r.Providers) > 0 {
		seen := make(map[string]struct{}, len(r.Providers))
		for _, raw := range r.Providers {
			providerID := strings.TrimSpace(raw)
			if providerID == "" {
				continue
			}
			if _, exists := ctx.providers[providerID]; !exists {
				return ctx.errf("map references unknown provider %q", providerID)
			}
			if _, duplicate := seen[providerID]; duplicate {
				return ctx.errf("provider %q appears more than once in the map rule", providerID)
			}
			seen[providerID] = struct{}{}
			ctx.st.pending = append(ctx.st.pending, Target{Provider: providerID, Model: r.Native, Tier: r.Tier})
		}
		ctx.st.sawMap = true
		return nil
	}

	if !ctx.st.hasSelection {
		return ctx.errf("map requires a preceding filter provider action (provider selection)")
	}
	if ctx.st.mapWithoutSelection {
		return ctx.errf("map requires a filter provider action after the previous map (provider selection)")
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
		ctx.st.pending = append(ctx.st.pending, Target{Provider: providerID, Model: r.Native, Tier: r.Tier})
	}
	ctx.st.sawMap = true
	// The selection is consumed by this map: the next map must be preceded by
	// a fresh filter provider action.
	ctx.st.mapWithoutSelection = true
	return nil
}
