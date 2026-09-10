package main

import "testing"

// TestRuleRegistryIsTheOnlyExtensionPoint proves that adding a new routing
// action requires no change to a shared rule struct or a central compiler
// switch: registering the action in ruleRegistry (plus the new implementation
// file) is enough for the strict decoder and the typed compiler dispatch. The
// strawman action reuses race semantics so the pipeline stays valid.
func TestRuleRegistryIsTheOnlyExtensionPoint(t *testing.T) {
	const strawAction = "straw"
	ruleRegistry[strawAction] = ruleDescriptor{rank: 6, new: func() Rule { return &RaceRule{} }}
	defer delete(ruleRegistry, strawAction)

	// Decode path: the envelope discriminates into the registered concrete
	// type without any switch extension.
	decoded := decodeRuleJSON(t, `{"route":"standard","action":"straw","count":1}`)
	if _, ok := decoded.(*RaceRule); !ok {
		t.Fatalf("registered action did not decode into its factory type: %T", decoded)
	}

	// Compile path: the rule applies itself to the builder state.
	r := &RaceRule{}
	r.setIdentity("standard", strawAction)
	r.Count = 1
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		r,
	)
	if entryRoute(t, result, "standard").RaceCount != 1 {
		t.Fatalf("new action was not dispatched through typed apply")
	}
}
