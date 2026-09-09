package main

import (
	"strings"
	"testing"
	"time"
)

func TestLeaseRuleDecode(t *testing.T) {
	r := decodeRuleJSON(t, `{
		"match":{"model":"standard"},"action":"lease",
		"source":"winner","duration":"10m","renew_on_success":false,
		"release_on":["429","5xx"],"release_after_slow_starts":3,"slow_start":"3s"
	}`)
	leased, ok := r.(*LeaseRule)
	if !ok {
		t.Fatalf("decoded rule is %T, want *LeaseRule", r)
	}
	if leased.Source != "winner" || leased.Duration.Duration != 10*time.Minute {
		t.Fatalf("lease fields mismatch: %#v", leased)
	}
	if leased.RenewOnSuccess == nil || *leased.RenewOnSuccess {
		t.Fatalf("renew_on_success mismatch: %#v", leased.RenewOnSuccess)
	}
	if len(leased.ReleaseOn) != 2 || leased.ReleaseAfterSlowStarts != 3 || leased.SlowStart.Duration != 3*time.Second {
		t.Fatalf("lease policy mismatch: %#v", leased)
	}
}

// leasePipeline returns map → rank → lease → race with the given lease policy.
func leaseRulesPipeline(r Rule) []Rule {
	return []Rule{poolRule("standard", "group"), rankRule("standard"), r, raceRule("standard", 2)}
}

func TestLeaseRuleCompile(t *testing.T) {
	plans, err := compileRules(leaseRulesPipeline(
		leaseRule("standard", func(r *LeaseRule) {
			r.Source = "winner"
			r.Duration = Duration{time.Minute}
			r.RenewOnSuccess = boolPtr(true)
			r.ReleaseOn = []string{"429", "5xx", "timeout", "connection_error"}
		}),
	)...)
	if err != nil {
		t.Fatal(err)
	}
	lease := plans["standard"].Lease
	if !lease.Enabled || lease.Duration != time.Minute || !lease.RenewOnSuccess {
		t.Fatalf("lease policy mismatch: %#v", lease)
	}
	if !lease.ReleaseOn[ErrorRateLimit] || !lease.ReleaseOn[ErrorUpstream] || lease.ReleaseOn[ErrorNotFound] {
		t.Fatalf("lease release_on mismatch: %#v", lease.ReleaseOn)
	}
}

func TestLeaseRuleRenewDefaultsTrue(t *testing.T) {
	plans, err := compileRules(leaseRulesPipeline(
		leaseRule("standard", func(r *LeaseRule) {
			r.Source = "winner"
			r.Duration = Duration{time.Minute}
		}),
	)...)
	if err != nil {
		t.Fatal(err)
	}
	if !plans["standard"].Lease.RenewOnSuccess {
		t.Fatalf("renew_on_success must default to true: %#v", plans["standard"].Lease)
	}
}

func TestLeaseRuleDecodeRejectsForeignField(t *testing.T) {
	decodeRuleError(t, `{
		"match":{"model":"standard"},"action":"lease",
		"source":"winner","duration":"10m","providers":["a"]
	}`, "unknown field \"providers\"", `action "lease"`)
}

func TestLeaseRuleRejectsBadSource(t *testing.T) {
	_, err := compileRules(leaseRulesPipeline(
		leaseRule("standard", func(r *LeaseRule) {
			r.Source = "loser"
			r.Duration = Duration{time.Minute}
		}),
	)...)
	if err == nil || !strings.Contains(err.Error(), "lease source") {
		t.Fatalf("expected lease source error, got %v", err)
	}
}

func TestLeaseRuleRejectsZeroDuration(t *testing.T) {
	_, err := compileRules(leaseRulesPipeline(
		leaseRule("standard", func(r *LeaseRule) { r.Source = "winner" }),
	)...)
	if err == nil || !strings.Contains(err.Error(), "lease duration") {
		t.Fatalf("expected lease duration error, got %v", err)
	}
}
