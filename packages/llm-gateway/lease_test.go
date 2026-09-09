package main

import (
	"testing"
	"time"
)

func TestLeaseStoreAcquireRenewExpire(t *testing.T) {
	now := time.Unix(1000, 0)
	store := newLeaseStore(func() time.Time { return now })
	if _, ok := store.Holder("standard"); ok {
		t.Fatal("fresh store must have no holder")
	}
	store.Renew("standard", "a", time.Minute)
	if holder, ok := store.Holder("standard"); !ok || holder != "a" {
		t.Fatalf("holder not acquired: %q %v", holder, ok)
	}
	now = now.Add(59 * time.Second)
	store.Renew("standard", "a", time.Minute)
	if _, ok := store.Holder("standard"); !ok {
		t.Fatal("renewed lease disappeared")
	}
	now = now.Add(time.Minute)
	if _, ok := store.Holder("standard"); ok {
		t.Fatal("expired lease still returned")
	}
	if store.HolderCount() != 0 {
		t.Fatalf("expired lease still counted: %d", store.HolderCount())
	}
}

func TestLeaseStoreReleaseOnlyHolder(t *testing.T) {
	store := newLeaseStore(time.Now)
	store.Renew("standard", "a", time.Minute)
	store.ReleaseIfHolder("standard", "b") // non-holder failure
	if holder, ok := store.Holder("standard"); !ok || holder != "a" {
		t.Fatalf("non-holder failure released the lease")
	}
	store.ReleaseIfHolder("standard", "a")
	if _, ok := store.Holder("standard"); ok {
		t.Fatal("holder failure did not release the lease")
	}
}

func TestLeaseStoreSlowStartThresholdAndReset(t *testing.T) {
	store := newLeaseStore(time.Now)
	store.Renew("standard", "a", time.Minute)
	store.ObserveSlowStart("standard", "a", 3)
	store.ObserveSlowStart("standard", "a", 3)
	if _, ok := store.Holder("standard"); !ok {
		t.Fatal("lease released before the slow-start threshold")
	}
	store.ObserveSlowStart("standard", "a", 3)
	if _, ok := store.Holder("standard"); ok {
		t.Fatal("lease survived the slow-start threshold")
	}
}

func TestLeaseStoreSlowStartResetOnFastWin(t *testing.T) {
	store := newLeaseStore(time.Now)
	store.Renew("standard", "a", time.Minute)
	store.ObserveSlowStart("standard", "a", 3)
	store.ResetSlowStarts("standard", "a")
	store.ObserveSlowStart("standard", "a", 3)
	store.ObserveSlowStart("standard", "a", 3)
	if holder, ok := store.Holder("standard"); !ok || holder != "a" {
		t.Fatal("reset did not break the consecutive slow-start counter")
	}
}

func TestLeaseStoreSlowStartByNonHolderIsNeutral(t *testing.T) {
	store := newLeaseStore(time.Now)
	store.Renew("standard", "a", time.Minute)
	store.ObserveSlowStart("standard", "b", 1)
	if holder, ok := store.Holder("standard"); !ok || holder != "a" {
		t.Fatalf("non-holder slow start affected the lease")
	}
}
