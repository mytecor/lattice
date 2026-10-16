package core

import (
	"context"
	"fmt"
)

// Subscribe registers one subject pattern. Each subscription has one worker and a
// bounded queue, so handler concurrency and memory remain finite.
func (b *Bus) Subscribe(pattern string, handler EventHandler) (*Subscription, error) {
	if err := validatePattern(pattern); err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, fmt.Errorf("%w: handler is required", ErrInvalidEvent)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrBusClosed
	}
	b.nextID++
	ctx, cancel := context.WithCancel(b.ctx)
	subscription := &Subscription{
		bus: b, id: b.nextID, pattern: pattern, handler: handler,
		queue: make(chan ReceivedEvent, b.config.QueueCapacity), ctx: ctx, cancel: cancel, done: make(chan struct{}),
	}
	subscription.wg.Add(1)
	b.subs[subscription.id] = subscription
	go subscription.run()
	return subscription, nil
}

func (s *Subscription) run() {
	defer func() {
		s.wg.Done()
		close(s.done)
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-s.queue:
			if err := s.handler(s.ctx, event); err != nil && s.bus.config.OnHandlerError != nil {
				s.bus.config.OnHandlerError(err)
			}
		}
	}
}

// Close removes the subscription and cancels its handler context.
func (s *Subscription) Close() error {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s.id)
		s.cancel()
		s.bus.mu.Unlock()
	})
	return nil
}

// Done closes after the subscription worker stops.
func (s *Subscription) Done() <-chan struct{} { return s.done }

func (b *Bus) patterns() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	seen := make(map[string]struct{}, len(b.subs))
	patterns := make([]string, 0, len(b.subs))
	for _, subscription := range b.subs {
		if _, ok := seen[subscription.pattern]; ok {
			continue
		}
		seen[subscription.pattern] = struct{}{}
		patterns = append(patterns, subscription.pattern)
	}
	return patterns
}
