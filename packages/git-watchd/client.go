package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mytecor/meshbus/core"
)

// Client provides a type-safe RPC client for git-watchd over Meshbus.
type Client struct {
	bus          Bus
	headSubject  string
	watchSubject string
}

// NewClient creates a new Client.
func NewClient(bus Bus, headSubject, watchSubject string) *Client {
	if headSubject == "" {
		headSubject = DefaultSubjectHead
	}
	if watchSubject == "" {
		watchSubject = DefaultSubjectWatch
	}
	return &Client{
		bus:          bus,
		headSubject:  headSubject,
		watchSubject: watchSubject,
	}
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Head requests the current revision of a local repo/ref.
func (c *Client) Head(ctx context.Context, repo, ref string) (*HeadResponse, error) {
	reqID := randomID()
	replyTo := fmt.Sprintf("git.reply.%s", reqID)

	respCh := make(chan *HeadResponse, 1)
	sub, err := c.bus.Subscribe(replyTo, func(_ context.Context, event core.ReceivedEvent) error {
		var resp HeadResponse
		if err := json.Unmarshal(event.Payload, &resp); err == nil && resp.ID == reqID {
			select {
			case respCh <- &resp:
			default:
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe reply: %w", err)
	}
	defer sub.Close()

	req := HeadRequest{
		ID:      reqID,
		Repo:    repo,
		Ref:     ref,
		ReplyTo: replyTo,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if _, err := c.bus.Publish(ctx, c.headSubject, payload, core.PublishOptions{
		TTL:         time.Minute,
		ContentType: "application/json",
	}); err != nil {
		return nil, fmt.Errorf("publish %s: %w", c.headSubject, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp.Error != "" {
			return resp, errors.New(resp.Error)
		}
		return resp, nil
	}
}

// Watch requests changes after afterRevision on repo/ref with a timeout.
func (c *Client) Watch(ctx context.Context, repo, ref, afterRevision string, timeout time.Duration) (*WatchResponse, error) {
	reqID := randomID()
	replyTo := fmt.Sprintf("git.reply.%s", reqID)

	respCh := make(chan *WatchResponse, 1)
	sub, err := c.bus.Subscribe(replyTo, func(_ context.Context, event core.ReceivedEvent) error {
		var resp WatchResponse
		if err := json.Unmarshal(event.Payload, &resp); err == nil && resp.ID == reqID {
			select {
			case respCh <- &resp:
			default:
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe reply: %w", err)
	}
	defer sub.Close()

	req := WatchRequest{
		ID:            reqID,
		Repo:          repo,
		Ref:           ref,
		AfterRevision: afterRevision,
		TimeoutMs:     timeout.Milliseconds(),
		ReplyTo:       replyTo,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if _, err := c.bus.Publish(ctx, c.watchSubject, payload, core.PublishOptions{
		TTL:         timeout + time.Minute,
		ContentType: "application/json",
	}); err != nil {
		return nil, fmt.Errorf("publish %s: %w", c.watchSubject, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp.Status == StatusError && resp.Error != "" {
			return resp, errors.New(resp.Error)
		}
		return resp, nil
	}
}
