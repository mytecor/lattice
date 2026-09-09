package main

import (
	"strings"
	"testing"
	"time"
)

func TestRetryRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"match":{"model":"standard"},"action":"retry",
		"scope":"next","count":1,"attempts":2,"on":["429","5xx"],
		"backoff":{"type":"exponential","initial":"200ms","max":"1s"}
	}`)
	retry, ok := r.(*RetryRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *RetryRule", r)
	}
	if retry.Scope != "next" || retry.Count != 1 || retry.Attempts != 2 || len(retry.On) != 2 {
		t.Fatalf("retry fields mismatch: %#v", retry)
	}
	if retry.Backoff == nil || retry.Backoff.Initial.Duration != 200*time.Millisecond {
		t.Fatalf("backoff mismatch: %#v", retry.Backoff)
	}
}

func TestRetryRuleCompileAppliesBackoffDefaults(t *testing.T) {
	plans, err := compileRules(append(basePipeline("standard"),
		retryRule("standard", func(r *RetryRule) {
			r.Scope = "next"
			r.Count = 1
			r.Attempts = 2
			r.On = []string{"429"}
		}),
	)...)
	if err != nil {
		t.Fatal(err)
	}
	retry := plans["standard"].Retry
	if retry.Scope != "next" || retry.Count != 1 || retry.Attempts != 2 {
		t.Fatalf("retry schedule mismatch: %#v", retry)
	}
	if retry.Backoff.Initial.Duration != 100*time.Millisecond || retry.Backoff.Max.Duration != time.Second {
		t.Fatalf("backoff defaults mismatch: %#v", retry.Backoff)
	}
}

func TestRetryRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"retry",
		"scope":"same","attempts":1,"after":"3s"
	}`, "unknown field \"after\"", `action "retry"`)
}

func TestRetryRuleRejectsZeroAttempts(t *testing.T) {
	_, err := compileRules(append(basePipeline("standard"),
		retryRule("standard", func(r *RetryRule) { r.Attempts = 0 }),
	)...)
	if err == nil || !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("expected attempts error, got %v", err)
	}
}

func TestRetryRuleNextRequiresCount(t *testing.T) {
	_, err := compileRules(append(basePipeline("standard"),
		retryRule("standard", func(r *RetryRule) {
			r.Scope = "next"
			r.Attempts = 1
		}),
	)...)
	if err == nil || !strings.Contains(err.Error(), "positive count") {
		t.Fatalf("expected next-count error, got %v", err)
	}
}
