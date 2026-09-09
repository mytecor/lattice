package main

import "testing"

// TestRuleRegistryIsTheOnlyExtensionPoint proves that adding a new routing
// action requires no change to a shared rule struct or a central compiler
// switch: registering the action in ruleRegistry (plus the new implementation
// file) is enough for the strict decoder and the typed compiler dispatch. The
// strawman action reuses race semantics so the pipeline stays valid.
func TestRuleRegistryIsTheOnlyExtensionPoint(t *testing.T) {
	const strawAction = "straw"
	ruleRegistry[strawAction] = ruleDescriptor{rank: 5, new: func() Rule { return &RaceRule{} }}
	defer delete(ruleRegistry, strawAction)

	// Decode path: the envelope discriminates into the registered concrete
	// type without any switch extension.
	decoded := decodeRuleJSON(t, `{"match":{"model":"standard"},"action":"straw","count":1}`)
	if _, ok := decoded.(*RaceRule); !ok {
		t.Fatalf("registered action did not decode into its factory type: %T", decoded)
	}

	// Compile path: the rule applies itself to the builder state.
	r := &RaceRule{}
	r.setIdentity("standard", strawAction)
	r.Count = 1
	plans, err := compileRules(poolRule("standard", "group"), rankRule("standard"), r)
	if err != nil {
		t.Fatal(err)
	}
	if plans["standard"].RaceCount != 1 {
		t.Fatalf("new action was not dispatched through typed apply: %#v", plans["standard"])
	}
}
