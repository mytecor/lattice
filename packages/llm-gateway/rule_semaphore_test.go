package main

import (
	"strings"
	"testing"
)

func TestSemaphoreRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"route":"standard","action":"semaphore",
		"max_calls":4,"max_in_flight":3,"max_calls_per_provider":1
	}`)
	sem, ok := r.(*SemaphoreRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *SemaphoreRule", r)
	}
	if sem.MaxCalls != 4 || sem.MaxInFlight != 3 || sem.MaxCallsPerProvider != 1 {
		t.Fatalf("semaphore fields mismatch: %#v", sem)
	}
}

func TestSemaphoreRuleCompile(t *testing.T) {
	result := mustCompile(t, append(standardEntry(), semaphoreRule("standard", 4, 3, 1))...)
	sem := entryRoute(t, result, "standard").Semaphore
	if sem.MaxCalls != 4 || sem.MaxInFlight != 3 || sem.MaxCallsPerProvider != 1 {
		t.Fatalf("semaphore limits mismatch: %#v", sem)
	}
}

func TestSemaphoreRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"route":"standard","action":"semaphore",
		"max_calls":4,"max_in_flight":3,"max_calls_per_provider":1,"duration":"10m"
	}`, "unknown field \"duration\"", `action "semaphore"`)
}

func TestSemaphoreRuleRejectsZeroBound(t *testing.T) {
	_, err := compileRules(append(standardEntry(), semaphoreRule("standard", 0, 1, 1))...)
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected positive bounds error, got %v", err)
	}
}

func TestSemaphoreRuleRejectedOnSubroute(t *testing.T) {
	_, err := compileRules(append(standardEntry(),
		retryRule("standard", "standard.retry", 1),
		filterError("standard.retry", "429"),
		filterProvider("standard.retry", "a", "b", "c"),
		mapRule("standard.retry", "native-model"),
		rankRule("standard.retry"),
		raceRule("standard.retry", 1),
		semaphoreRule("standard.retry", 4, 3, 1),
	)...)
	if err == nil || !strings.Contains(err.Error(), "request-wide") {
		t.Fatalf("expected request-wide subroute rejection, got %v", err)
	}
}
