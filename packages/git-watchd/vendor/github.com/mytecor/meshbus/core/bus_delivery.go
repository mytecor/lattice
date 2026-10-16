package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// HandleMessage consumes meshbus event frames and leaves other direct messages
// to the caller. A true result means the frame belonged to pub/sub even when it
// was expired, duplicated, invalid, or backpressured.
func (b *Bus) HandleMessage(_ context.Context, message ReceivedMessage) (bool, error) {
	frame := message.Payload()
	if !isEventMessage(frame) {
		return false, nil
	}
	event, err := decodeEvent(frame, b.config.MaxPayloadBytes, b.config.MaxTTL)
	if err != nil {
		return true, err
	}
	now := b.config.clock().UTC()
	expiresAt := event.PublishedAt.Add(event.TTL)
	if !expiresAt.After(now) {
		return true, nil
	}
	receiveBound := now.Add(event.TTL)
	if receiveBound.Before(expiresAt) {
		expiresAt = receiveBound
	}
	if b.duplicate(event.ID, expiresAt, now) {
		return true, nil
	}
	return true, b.dispatch(ReceivedEvent{Event: event, Sender: message.Sender(), ReceivedAt: now})
}

// Handler composes pub/sub with a fallback direct-message handler.
func (b *Bus) Handler(next Handler) Handler {
	return func(ctx context.Context, message ReceivedMessage) error {
		handled, err := b.HandleMessage(ctx, message)
		if handled || err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		return next(ctx, message)
	}
}

func (b *Bus) duplicate(id EventID, expiresAt, now time.Time) bool {
	b.dedupMu.Lock()
	defer b.dedupMu.Unlock()
	for candidate, expiry := range b.seen {
		if !expiry.After(now) {
			delete(b.seen, candidate)
		}
	}
	if _, exists := b.seen[id]; exists {
		return true
	}
	if len(b.seen) >= b.config.DedupCapacity {
		var oldest EventID
		var oldestExpiry time.Time
		for candidate, expiry := range b.seen {
			if oldestExpiry.IsZero() || expiry.Before(oldestExpiry) {
				oldest, oldestExpiry = candidate, expiry
			}
		}
		delete(b.seen, oldest)
	}
	b.seen[id] = expiresAt
	return false
}

func (b *Bus) dispatch(event ReceivedEvent) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBusClosed
	}
	var failures []error
	for _, subscription := range b.subs {
		if !matchPattern(subscription.pattern, event.Topic) {
			continue
		}
		select {
		case subscription.queue <- cloneReceivedEvent(event):
		default:
			failures = append(failures, fmt.Errorf("%w: topic %q", ErrEventBackpressure, event.Topic))
		}
	}
	return errors.Join(failures...)
}
