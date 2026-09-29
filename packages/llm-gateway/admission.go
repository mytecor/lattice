package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type AdmissionController struct {
	mu      sync.Mutex
	routes  map[string]*admissionQueue
	metrics *Metrics
	now     func() time.Time
}

type admissionQueue struct {
	inFlight int
	waiters  []chan struct{}
}

func newAdmissionController(metrics *Metrics, now func() time.Time) *AdmissionController {
	if now == nil {
		now = time.Now
	}
	return &AdmissionController{
		routes:  make(map[string]*admissionQueue),
		metrics: metrics,
		now:     now,
	}
}

func (ac *AdmissionController) Admit(ctx context.Context, route string, policy AdmissionConfig, deadline time.Time) (func(), *CallError) {
	if policy.MaxInFlight <= 0 {
		return func() {}, nil
	}
	ac.mu.Lock()
	q, ok := ac.routes[route]
	if !ok {
		q = &admissionQueue{}
		ac.routes[route] = q
	}

	if q.inFlight < policy.MaxInFlight {
		q.inFlight++
		if ac.metrics != nil {
			ac.metrics.ObserveRouteInFlight(route, q.inFlight)
		}
		ac.mu.Unlock()
		return ac.makeReleaser(route), nil
	}

	// Queue check
	if policy.MaxPending <= 0 || len(q.waiters) >= policy.MaxPending {
		ac.mu.Unlock()
		if ac.metrics != nil {
			ac.metrics.ObserveRouteQueueRejection(route, "max_pending")
		}
		return nil, &CallError{
			Class:  ErrorRateLimit,
			Status: http.StatusTooManyRequests,
			Cause:  fmt.Errorf("route %q queue is full (max_pending=%d)", route, policy.MaxPending),
		}
	}

	ch := make(chan struct{}, 1)
	q.waiters = append(q.waiters, ch)
	waitStart := ac.now()
	if ac.metrics != nil {
		ac.metrics.ObserveRoutePending(route, len(q.waiters))
	}
	ac.mu.Unlock()

	waitTimeout := policy.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = 30 * time.Second
	}
	deadlineLimited := false
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			ac.mu.Lock()
			ac.removeWaiter(q, route, ch)
			ac.mu.Unlock()
			return nil, routeTimeoutError()
		}
		if remaining < waitTimeout {
			waitTimeout = remaining
			deadlineLimited = true
		}
	}
	timer := time.NewTimer(waitTimeout)
	defer timer.Stop()

	select {
	case <-ch:
		if ac.metrics != nil {
			ac.metrics.ObserveRouteQueueWait(route, ac.now().Sub(waitStart))
		}
		return ac.makeReleaser(route), nil

	case <-ctx.Done():
		ac.mu.Lock()
		ac.removeWaiter(q, route, ch)
		ac.mu.Unlock()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, routeTimeoutError()
		}
		return nil, &CallError{Class: ErrorCancelled, Status: 499, Cause: ctx.Err()}

	case <-timer.C:
		ac.mu.Lock()
		ac.removeWaiter(q, route, ch)
		ac.mu.Unlock()
		if deadlineLimited {
			return nil, routeTimeoutError()
		}
		if ac.metrics != nil {
			ac.metrics.ObserveRouteQueueRejection(route, "wait_timeout")
		}
		return nil, &CallError{
			Class:  ErrorTimeout,
			Status: http.StatusGatewayTimeout,
			Cause:  fmt.Errorf("route %q queue wait timeout (%s) exceeded", route, waitTimeout),
		}
	}
}

func (ac *AdmissionController) removeWaiter(q *admissionQueue, route string, target chan struct{}) {
	for i, w := range q.waiters {
		if w == target {
			q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
			break
		}
	}
	if ac.metrics != nil {
		ac.metrics.ObserveRoutePending(route, len(q.waiters))
	}
	// If a slot was sent to target right before cancel/timeout, pass it along
	select {
	case <-target:
		if len(q.waiters) > 0 {
			next := q.waiters[0]
			q.waiters = q.waiters[1:]
			if ac.metrics != nil {
				ac.metrics.ObserveRoutePending(route, len(q.waiters))
			}
			next <- struct{}{}
		} else {
			q.inFlight--
			if ac.metrics != nil {
				ac.metrics.ObserveRouteInFlight(route, q.inFlight)
			}
		}
	default:
	}
}

func (ac *AdmissionController) makeReleaser(route string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			ac.mu.Lock()
			defer ac.mu.Unlock()
			q, ok := ac.routes[route]
			if !ok {
				return
			}
			if len(q.waiters) > 0 {
				next := q.waiters[0]
				q.waiters = q.waiters[1:]
				if ac.metrics != nil {
					ac.metrics.ObserveRoutePending(route, len(q.waiters))
				}
				next <- struct{}{}
			} else {
				q.inFlight--
				if ac.metrics != nil {
					ac.metrics.ObserveRouteInFlight(route, q.inFlight)
				}
			}
		})
	}
}
