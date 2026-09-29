package main

import (
	"time"
)

type AdmissionConfig struct {
	MaxInFlight int
	MaxPending  int
	WaitTimeout time.Duration
}

// AdmissionRule declares the admission and queueing policy on a route.
// It bounds concurrent in-flight requests and sets queueing limits for pending requests.
type AdmissionRule struct {
	ruleBase
	MaxInFlight      int      `json:"max_in_flight,omitempty"`
	MaxInFlightCamel int      `json:"maxInFlight,omitempty"`
	MaxPending       *int     `json:"max_pending,omitempty"`
	MaxPendingCamel  *int     `json:"maxPending,omitempty"`
	WaitTimeout      Duration `json:"wait_timeout,omitempty"`
	WaitTimeoutCamel Duration `json:"waitTimeout,omitempty"`
}

func (r *AdmissionRule) apply(ctx *stageContext) error {
	maxInFlight := r.MaxInFlight
	if maxInFlight == 0 && r.MaxInFlightCamel != 0 {
		maxInFlight = r.MaxInFlightCamel
	}
	maxPending := -1
	if r.MaxPending != nil {
		maxPending = *r.MaxPending
	} else if r.MaxPendingCamel != nil {
		maxPending = *r.MaxPendingCamel
	}
	waitTimeout := r.WaitTimeout.Duration
	if waitTimeout == 0 && r.WaitTimeoutCamel.Duration != 0 {
		waitTimeout = r.WaitTimeoutCamel.Duration
	}
	if maxInFlight < 0 {
		return ctx.errf("admission max_in_flight must not be negative")
	}
	if maxPending < -1 {
		return ctx.errf("admission max_pending must not be negative")
	}
	if waitTimeout < 0 {
		return ctx.errf("admission wait_timeout must not be negative")
	}
	if waitTimeout == 0 {
		waitTimeout = 30 * time.Second
	}
	if maxPending < 0 {
		if maxInFlight > 0 {
			maxPending = 4 * maxInFlight
		} else {
			maxPending = 16
		}
	}
	ctx.plan.Admission = AdmissionConfig{
		MaxInFlight: maxInFlight,
		MaxPending:  maxPending,
		WaitTimeout: waitTimeout,
	}
	return nil
}
