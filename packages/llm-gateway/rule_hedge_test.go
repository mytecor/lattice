package main

import (
	"strings"
	"testing"
	"time"
)

func TestHedgeRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{"match":{"model":"standard"},"action":"hedge","after":"3s"}`)
	hedged, ok := r.(*HedgeRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *HedgeRule", r)
	}
	if hedged.After.Duration != 3*time.Second {
		t.Fatalf("hedge delay mismatch: %#v", hedged)
	}
}

func TestHedgeRuleCompile(t *testing.T) {
	plans, err := compileRules(append(basePipeline("standard"), hedgeRule("standard", 3*time.Second))...)
	if err != nil {
		t.Fatal(err)
	}
	if plans["standard"].HedgeAfter != 3*time.Second {
		t.Fatalf("hedge delay mismatch: %s", plans["standard"].HedgeAfter)
	}
}

func TestHedgeRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"hedge","after":"3s","count":2
	}`, "unknown field \"count\"", `action "hedge"`)
}

func TestHedgeRuleRejectsZeroAfter(t *testing.T) {
	_, err := compileRules(append(basePipeline("standard"), hedgeRule("standard", 0))...)
	if err == nil || !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "migration") {
		t.Fatalf("expected parameterless hedge error, got %v", err)
	}
}
