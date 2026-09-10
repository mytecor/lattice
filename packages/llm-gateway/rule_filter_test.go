package main

import (
	"strings"
	"testing"
)

func TestFilterRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"filter","where":{"provider":{"in":["a","b"]}}
	}`)
	filter, ok := r.(*FilterRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *FilterRule", r)
	}
	if filter.route() != "standard" || filter.action() != "filter" {
		t.Fatalf("identity mismatch: %#v", filter)
	}
	if filter.Where.Provider == nil || filter.Where.Provider.In == nil || len(*filter.Where.Provider.In) != 2 {
		t.Fatalf("provider filter mismatch: %#v", filter.Where)
	}
}

func TestFilterRuleModelDecodeAndCompile(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	route := entryRoute(t, result, "standard")
	if !route.Entry || route.ModelEq != "standard" {
		t.Fatalf("entry route not derived from the model filter: %#v", route)
	}
	if got := targetIDs(route.Pool); got != "a,b" {
		t.Fatalf("provider in filter did not build the selection: %s", got)
	}
}

func TestFilterRuleProviderNotIn(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b", "c"),
		filterProviderNotIn("standard", "a"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	route := entryRoute(t, result, "standard")
	if got := targetIDs(route.Pool); got != "b,c" {
		t.Fatalf("not_in filter must exclude from the universe: %s", got)
	}
}

func TestFilterRuleErrorInDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard.retry","action":"filter",
		"where":{"error":{"in":["429","5xx","timeout","connection_error"]}}
	}`)
	filter, ok := r.(*FilterRule)
	if !ok || filter.Where.Error == nil {
		t.Fatalf("decoded rule is %T with %#v", r, filter)
	}
	if len(filter.Where.Error.In) != 4 {
		t.Fatalf("error filter mismatch: %#v", filter.Where.Error)
	}
}

func TestFilterRuleErrorGatesTransition(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		retryRule("standard", "standard.retry", 1),
		// retry subroute: only rate limits and upstream errors are retryable.
		filterError("standard.retry", "429", "5xx"),
		filterProviderUnused("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
	)
	retryRoute := result.routes["standard.retry"]
	if !retryRoute.ErrorIn[ErrorRateLimit] || !retryRoute.ErrorIn[ErrorUpstream] {
		t.Fatalf("error filter not compiled into the target route: %#v", retryRoute.ErrorIn)
	}
	if retryRoute.ErrorIn[ErrorTimeout] || retryRoute.ErrorIn[ErrorModelNotFound] {
		t.Fatalf("error filter must be exact, not substring-based: %#v", retryRoute.ErrorIn)
	}
	if !retryRoute.applicable(&CallError{Class: ErrorRateLimit}, 0) {
		t.Fatalf("429 must admit the retry transition")
	}
	if retryRoute.applicable(&CallError{Class: ErrorInvalid}, 0) {
		t.Fatalf("invalid_response must not admit the retry transition")
	}
	if !retryRoute.ProviderUnused {
		t.Fatalf("unused provider policy not compiled: %#v", retryRoute)
	}
}

func TestFilterRuleAttemptBoundsTransition(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
		retryRule("standard", "standard.retry", 3),
		filterError("standard.retry", "429"),
		filterAttempt("standard.retry", 3),
		filterProvider("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
	)
	route := result.routes["standard.retry"]
	if route.AttemptLT != 3 {
		t.Fatalf("attempt filter mismatch: %d", route.AttemptLT)
	}
	if !route.applicable(&CallError{Class: ErrorRateLimit}, 0) || !route.applicable(&CallError{Class: ErrorRateLimit}, 2) {
		t.Fatalf("attempts 0..2 must be applicable")
	}
	if route.applicable(&CallError{Class: ErrorRateLimit}, 3) {
		t.Fatalf("attempt 3 must be blocked by the attempt filter")
	}
}

func TestFilterRuleConsecutiveProviderFiltersReplaceSelection(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		filterProvider("standard", "c"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 1),
	)
	route := entryRoute(t, result, "standard")
	// The later provider filter re-selects from the route provider universe:
	// it must not permanently remove a,b (the earlier filter), it replaces it.
	if got := targetIDs(route.Pool); got != "c" {
		t.Fatalf("consecutive filter must replace the selection: %s", got)
	}
}

func TestFilterRuleDifferentProviderGroupsMapToDifferentNatives(t *testing.T) {
	result := mustCompile(t,
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-x"),
		filterProvider("standard", "c"),
		mapRule("standard", "native-y"),
		rankRule("standard"),
		raceRule("standard", 3),
	)
	route := entryRoute(t, result, "standard")
	if len(route.Pool) != 3 {
		t.Fatalf("expected 3 candidates, got %#v", route.Pool)
	}
	byProvider := map[string]string{}
	for _, target := range route.Pool {
		byProvider[target.Provider] = target.Model
	}
	if byProvider["a"] != "native-x" || byProvider["b"] != "native-x" || byProvider["c"] != "native-y" {
		t.Fatalf("provider groups must map to different natives: %#v", byProvider)
	}
}

func TestFilterRuleDecodeRejectsUnknownFilterField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"filter","where":{"provider":{"in":["a"],"bogus":true}}
	}`, "unknown field \"bogus\"", `action "filter"`)
}

func TestFilterRuleDecodeRejectsUnknownOperator(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"filter","where":{"model":{"ne":"standard"}}
	}`, "unknown field \"ne\"", `action "filter"`)
}

func TestFilterRuleDecodeRejectsInvalidValueType(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"filter","where":{"provider":{"in":"a"}}
	}`, "cannot unmarshal string", `action "filter"`)
}

func TestFilterRuleRejectsExplicitEmptyProviderIn(t *testing.T) {
	empty := decodeRuleJSON(t, `{
		"route":"standard","action":"filter","where":{"provider":{"in":[]}}
	}`)
	_, err := compileRules(
		filterModel("standard", "standard"),
		filterProvider("standard", "a", "b"),
		empty,
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "non-empty in list") {
		t.Fatalf("expected explicit empty provider in rejection, got %v", err)
	}
}

func TestFilterRuleRejectsMissingWhere(t *testing.T) {
	filter := &FilterRule{}
	filter.setIdentity("standard", "filter")
	_, err := compileRules(
		filter,
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected missing where-dimension error, got %v", err)
	}
}

func TestFilterRuleRejectsMultipleDimensions(t *testing.T) {
	filter := &FilterRule{}
	filter.setIdentity("standard", "filter")
	filter.Where.Model = &filterModelCond{Eq: "standard"}
	in := []string{"a"}
	filter.Where.Provider = &filterProviderCond{In: &in}
	_, err := compileRules(
		filter,
		filterProvider("standard", "a", "b"),
		mapRule("standard", "native-model"),
		rankRule("standard"),
		raceRule("standard", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected single-dimension error, got %v", err)
	}
}
