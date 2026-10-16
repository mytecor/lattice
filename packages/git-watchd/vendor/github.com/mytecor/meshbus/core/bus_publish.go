package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Publish creates one event and sends it once to each peer whose non-expired
// leased interests match the concrete topic.
func (b *Bus) Publish(ctx context.Context, topic string, payload []byte, options PublishOptions) (PublishResult, error) {
	if err := ctx.Err(); err != nil {
		return PublishResult{}, err
	}
	if err := validateTopic(topic); err != nil {
		return PublishResult{}, err
	}
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return PublishResult{}, ErrBusClosed
	}
	if options.TTL == 0 {
		options.TTL = b.config.DefaultTTL
	}
	var id EventID
	if _, err := io.ReadFull(b.config.idSource, id[:]); err != nil {
		return PublishResult{}, fmt.Errorf("generate event ID: %w", err)
	}
	event := Event{
		ID: id, Topic: topic, PublishedAt: b.config.clock().UTC(), TTL: options.TTL,
		ContentType: options.ContentType, Payload: payload,
	}
	frame, err := encodeEvent(event, b.config.MaxPayloadBytes, b.config.MaxTTL)
	if err != nil {
		return PublishResult{}, err
	}
	result := PublishResult{ID: id, Failed: make(map[PeerID]error)}
	var failures []error
	if !options.localSender.IsZero() {
		receivedAt := b.config.clock().UTC()
		b.duplicate(id, event.PublishedAt.Add(event.TTL), receivedAt)
		local := ReceivedEvent{Event: cloneEvent(event), Sender: options.localSender, ReceivedAt: receivedAt}
		if localErr := b.dispatch(local); localErr != nil {
			result.LocalError = localErr
			failures = append(failures, localErr)
		} else {
			result.LocalDelivered = true
		}
	}
	destinations := b.destinations(topic)
	result.Attempted = len(destinations)
	if len(destinations) == 0 {
		return result, errors.Join(failures...)
	}

	type sendResult struct {
		peer PeerID
		err  error
	}
	jobs := make(chan PeerID, len(destinations))
	results := make(chan sendResult, len(destinations))
	for _, destination := range destinations {
		jobs <- destination
	}
	close(jobs)
	workers := min(b.config.FanoutConcurrency, len(destinations))
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for peer := range jobs {
				if sendErr := b.sender.SendMessage(ctx, peer, bytes.Clone(frame)); sendErr != nil {
					results <- sendResult{peer: peer, err: fmt.Errorf("publish event to %s: %w", peer, sendErr)}
				} else {
					results <- sendResult{peer: peer}
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	for delivery := range results {
		if delivery.err != nil {
			result.Failed[delivery.peer] = delivery.err
			failures = append(failures, delivery.err)
		} else {
			result.Delivered++
		}
	}
	return result, errors.Join(failures...)
}

func (b *Bus) destinations(topic string) []PeerID {
	seen := make(map[PeerID]struct{})
	result := make([]PeerID, 0)
	for _, peer := range b.interests.InterestedPeers(topic) {
		if peer.IsZero() {
			continue
		}
		if _, exists := seen[peer]; exists {
			continue
		}
		seen[peer] = struct{}{}
		result = append(result, peer)
	}
	return result
}
