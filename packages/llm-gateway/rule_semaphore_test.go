package main

import (
	"strings"
	"testing"
)

func TestSemaphoreRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"match":{"model":"standard"},"action":"semaphore",
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
	plans, err := compileRules(append(basePipeline("standard"), semaphoreRule("standard", 4, 3, 1))...)
	if err != nil {
		t.Fatal(err)
	}
	sem := plans["standard"].Semaphore
	if sem.MaxCalls != 4 || sem.MaxInFlight != 3 || sem.MaxCallsPerProvider != 1 {
		t.Fatalf("semaphore limits mismatch: %#v", sem)
	}
}

func TestSemaphoreRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"semaphore",
		"max_calls":4,"max_in_flight":3,"max_calls_per_provider":1,"duration":"10m"
	}`, "unknown field \"duration\"", `action "semaphore"`)
}

func TestSemaphoreRuleRejectsZeroBound(t *testing.T) {
	_, err := compileRules(append(basePipeline("standard"), semaphoreRule("standard", 0, 1, 1))...)
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected positive bounds error, got %v", err)
	}
}
