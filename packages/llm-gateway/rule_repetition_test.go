package main

import "testing"

// repetitionEntry returns a minimal valid streaming entry route for logical
// model "standard" that also declares the loop-guard ("repetition") policy,
// optionally alongside the continue policy that the guard drives through.
func repetitionEntry() []Rule {
	return []Rule{
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		repetitionRule("standard"),
	}
}

func repetitionRule(route string) Rule {
	r := &RepetitionRule{}
	r.setIdentity(route, "repetition")
	return r
}

func TestRepetitionRuleCompilesWithDefaults(t *testing.T) {
	res := mustCompile(t, repetitionEntry()...)
	route := entryRoute(t, res, "standard")
	if !route.Repetition.Enabled {
		t.Fatalf("repetition must be enabled after a repetition action")
	}
	if route.Repetition.Repeats != defaultRepetitionRepeats {
		t.Fatalf("repetition repeats default = %d, want %d", route.Repetition.Repeats, defaultRepetitionRepeats)
	}
	if route.Repetition.MinLen != defaultRepetitionMinLen {
		t.Fatalf("repetition min_len default = %d, want %d", route.Repetition.MinLen, defaultRepetitionMinLen)
	}
	if route.Repetition.MaxLen != defaultRepetitionMaxLen {
		t.Fatalf("repetition max_len default = %d, want %d", route.Repetition.MaxLen, defaultRepetitionMaxLen)
	}
}

func TestRepetitionRuleDoesNotEnableByDefault(t *testing.T) {
	// An absent repetition rule must arm nothing: the canonical entry route
	// without the action has detection disabled, exactly like continue.
	res := mustCompile(t, standardEntry()...)
	route := entryRoute(t, res, "standard")
	if route.Repetition.Enabled {
		t.Fatal("repetition must be disabled when no repetition action is declared")
	}
}

func TestRepetitionRuleExplicitConfig(t *testing.T) {
	r := repetitionRule("standard").(*RepetitionRule)
	r.Repeats = 3
	r.MinLen = 8
	r.MaxLen = 512
	res := mustCompile(t, append(standardEntry(), r)...)
	route := entryRoute(t, res, "standard")
	if route.Repetition.Repeats != 3 || route.Repetition.MinLen != 8 || route.Repetition.MaxLen != 512 {
		t.Fatalf("repetition config = %+v, want repeats=3 min_len=8 max_len=512", route.Repetition)
	}
}

func TestRepetitionRuleRejectsBadRepeats(t *testing.T) {
	// Below the minimum K (2) is meaningless — one repeated fragment is
	// ordinary speech.
	r := repetitionRule("standard").(*RepetitionRule)
	r.Repeats = 1
	if _, err := compileRules(r); err == nil {
		t.Fatal("repetition with repeats < 2 must fail")
	}
	r = repetitionRule("standard").(*RepetitionRule)
	r.Repeats = maxRepetitionRepeats + 1
	if _, err := compileRules(r); err == nil {
		t.Fatal("repetition with repeats above the cap must fail")
	}
}

func TestRepetitionRuleRejectsBadMinLen(t *testing.T) {
	r := repetitionRule("standard").(*RepetitionRule)
	r.MinLen = 1
	if _, err := compileRules(r); err == nil {
		t.Fatal("repetition with min_len below 2 must fail")
	}
}

func TestRepetitionRuleRejectsMinGteMax(t *testing.T) {
	r := repetitionRule("standard").(*RepetitionRule)
	r.MinLen = 100
	r.MaxLen = 100
	if _, err := compileRules(r); err == nil {
		t.Fatal("repetition with min_len >= max_len must fail")
	}
	r = repetitionRule("standard").(*RepetitionRule)
	r.MinLen = 50
	r.MaxLen = 40
	if _, err := compileRules(r); err == nil {
		t.Fatal("repetition with min_len > max_len must fail")
	}
}

func TestRepetitionRuleRejectsBadMaxLen(t *testing.T) {
	r := repetitionRule("standard").(*RepetitionRule)
	r.MaxLen = maxRepetitionMaxLen + 1
	if _, err := compileRules(r); err == nil {
		t.Fatal("repetition with max_len above the cap must fail")
	}
}

func TestRepetitionRuleRequiresEntryRoute(t *testing.T) {
	// Like continue, the loop guard belongs to the discoverable entry model:
	// a repetition action on a subroute must fail.
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		repetitionRule("standard.retry"),
	)
	if err == nil {
		t.Fatal("repetition on a subroute without an entry filter must fail")
	}
}

func TestRepetitionRuleRequiresRace(t *testing.T) {
	// A repetition action before the route's race action is a config error.
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		repetitionRule("standard"),
	)
	if err == nil {
		t.Fatal("repetition without a preceding race action must fail")
	}
}

func TestRepetitionRuleRegistered(t *testing.T) {
	if _, ok := ruleRegistry[ActionRepetition]; !ok {
		t.Fatal("repetition action not registered")
	}
}

func TestRepetitionRuleDecodesNixShape(t *testing.T) {
	// The Nix module projects explicit fields; absent fields resolve to the
	// gateway defaults.
	rule := decodeRuleJSON(t, `{"route":"smart","action":"repetition","repeats":4,"min_len":6,"max_len":256}`)
	rr, ok := rule.(*RepetitionRule)
	if !ok {
		t.Fatalf("decoded type = %T, want *RepetitionRule", rule)
	}
	if rr.Repeats != 4 || rr.MinLen != 6 || rr.MaxLen != 256 {
		t.Fatalf("decoded repetition = repeats=%d min_len=%d max_len=%d, want 4/6/256",
			rr.Repeats, rr.MinLen, rr.MaxLen)
	}
	// A bare action also decodes (all fields optional, defaults applied).
	rule = decodeRuleJSON(t, `{"route":"smart","action":"repetition"}`)
	if _, ok := rule.(*RepetitionRule); !ok {
		t.Fatalf("bare repetition decoded type = %T, want *RepetitionRule", rule)
	}
}

func TestRepetitionRuleDecodeRejectsForeignFields(t *testing.T) {
	// A field owned by another action must be rejected on the decode boundary.
	decodeRuleError(t, `{"route":"smart","action":"repetition","idle":"90s"}`, "repetition")
}
