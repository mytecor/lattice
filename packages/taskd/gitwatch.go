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

// Default subjects for git-watchd.
const (
	DefaultSubjectHead  = "git.head"
	DefaultSubjectWatch = "git.watch"
)

// Watch response statuses.
const (
	StatusChanged  = "changed"
	StatusNoChange = "no_change"
	StatusError    = "error"
)

// HeadRequest represents a request to resolve current HEAD of a local repo/ref.
type HeadRequest struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Ref     string `json:"ref"`
	ReplyTo string `json:"reply_to"`
}

// HeadResponse represents the reply to a HeadRequest.
type HeadResponse struct {
	ID       string `json:"id"`
	Revision string `json:"revision,omitempty"`
	Error    string `json:"error,omitempty"`
}

// WatchRequest represents a request to watch for changes after after_revision.
type WatchRequest struct {
	ID            string `json:"id"`
	Repo          string `json:"repo"`
	Ref           string `json:"ref"`
	AfterRevision string `json:"after_revision,omitempty"`
	TimeoutMs     int64  `json:"timeout_ms,omitempty"`
	ReplyTo       string `json:"reply_to"`
}

// WatchResponse represents the reply to a WatchRequest.
type WatchResponse struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"` // "changed", "no_change", "error"
	From         string   `json:"from,omitempty"`
	To           string   `json:"to,omitempty"`
	ChangedPaths []string `json:"changed_paths,omitempty"`
	Revision     string   `json:"revision,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// Bus abstracts publish/subscribe operations (satisfied by *meshrns.Node).
type Bus interface {
	Publish(ctx context.Context, topic string, payload []byte, options core.PublishOptions) (core.PublishResult, error)
	Subscribe(pattern string, handler core.EventHandler) (*core.Subscription, error)
}

// GitWatcher defines the RPC contract with git-watchd.
type GitWatcher interface {
	Head(ctx context.Context, repo, ref string) (string, error)
	Watch(ctx context.Context, repo, ref, afterRevision string, timeout time.Duration) (*WatchResponse, error)
}

// MeshbusGitWatcher implements GitWatcher over Meshbus.
type MeshbusGitWatcher struct {
	bus          Bus
	headSubject  string
	watchSubject string
}

// NewMeshbusGitWatcher creates a new MeshbusGitWatcher.
func NewMeshbusGitWatcher(bus Bus, headSubject, watchSubject string) *MeshbusGitWatcher {
	if headSubject == "" {
		headSubject = DefaultSubjectHead
	}
	if watchSubject == "" {
		watchSubject = DefaultSubjectWatch
	}
	return &MeshbusGitWatcher{
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
func (w *MeshbusGitWatcher) Head(ctx context.Context, repo, ref string) (string, error) {
	reqID := randomID()
	replyTo := fmt.Sprintf("taskd.git.reply.%s", reqID)

	respCh := make(chan *HeadResponse, 1)
	sub, err := w.bus.Subscribe(replyTo, func(_ context.Context, event core.ReceivedEvent) error {
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
		return "", fmt.Errorf("subscribe reply: %w", err)
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
		return "", err
	}

	if _, err := w.bus.Publish(ctx, w.headSubject, payload, core.PublishOptions{
		TTL:         time.Minute,
		ContentType: "application/json",
	}); err != nil {
		return "", fmt.Errorf("publish %s: %w", w.headSubject, err)
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resp := <-respCh:
		if resp.Error != "" {
			return "", errors.New(resp.Error)
		}
		if resp.Revision == "" {
			return "", errors.New("empty revision received from git-watchd")
		}
		return resp.Revision, nil
	}
}

// Watch requests revision changes from git-watchd after afterRevision.
func (w *MeshbusGitWatcher) Watch(ctx context.Context, repo, ref, afterRevision string, timeout time.Duration) (*WatchResponse, error) {
	reqID := randomID()
	replyTo := fmt.Sprintf("taskd.git.reply.%s", reqID)

	respCh := make(chan *WatchResponse, 1)
	sub, err := w.bus.Subscribe(replyTo, func(_ context.Context, event core.ReceivedEvent) error {
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

	if _, err := w.bus.Publish(ctx, w.watchSubject, payload, core.PublishOptions{
		TTL:         timeout + time.Minute,
		ContentType: "application/json",
	}); err != nil {
		return nil, fmt.Errorf("publish %s: %w", w.watchSubject, err)
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
