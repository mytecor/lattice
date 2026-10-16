package rns

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/Quad4-Software/Reticulum-Go/pkg/channel"
	"github.com/mytecor/meshbus/core"
)

var ErrRealmAuthentication = errors.New("realm authentication failed")

func (e *Endpoint) beginAuthentication(active *session) {
	active.mu.Lock()
	if len(active.challenge) != 0 || active.authErr != nil || active.authenticated {
		active.mu.Unlock()
		return
	}
	nonce := make([]byte, authNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		active.mu.Unlock()
		e.completeAuthentication(active, fmt.Errorf("%w: generate nonce: %v", ErrRealmAuthentication, err))
		return
	}
	active.challenge = nonce
	active.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), e.networkWait)
	defer cancel()
	if err := e.sendChannel(ctx, active, &authMessage{kind: authKindChallenge, nonce: nonce}); err != nil {
		e.completeAuthentication(active, fmt.Errorf("%w: send challenge: %v", ErrRealmAuthentication, err))
		active.link.Teardown()
	}
}

func (e *Endpoint) handleAuthentication(active *session, message *authMessage) {
	active.mu.Lock()
	if len(active.sender) == 0 {
		if active.pendingAuth == nil {
			active.pendingAuth = make(map[byte]authMessage)
		}
		active.pendingAuth[message.kind] = authMessage{
			kind: message.kind, nonce: bytes.Clone(message.nonce), proof: bytes.Clone(message.proof),
		}
		active.mu.Unlock()
		return
	}
	sender := bytes.Clone(active.sender)
	challenge := bytes.Clone(active.challenge)
	active.mu.Unlock()
	switch message.kind {
	case authKindChallenge:
		proof, err := e.realm.Proof(message.nonce, sender, e.identity.Hash())
		if err != nil {
			e.completeAuthentication(active, fmt.Errorf("%w: create proof: %v", ErrRealmAuthentication, err))
			active.link.Teardown()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), e.networkWait)
		defer cancel()
		if err := e.sendChannel(ctx, active, &authMessage{kind: authKindResponse, nonce: message.nonce, proof: proof}); err != nil {
			e.completeAuthentication(active, fmt.Errorf("%w: send response: %v", ErrRealmAuthentication, err))
			active.link.Teardown()
		}
	case authKindResponse:
		if len(challenge) != authNonceSize || !bytes.Equal(challenge, message.nonce) ||
			!e.realm.Verify(message.proof, challenge, e.identity.Hash(), sender) {
			e.completeAuthentication(active, ErrRealmAuthentication)
			active.link.Teardown()
			return
		}
		e.completeAuthentication(active, nil)
	case authKindReady:
		active.mu.Lock()
		active.peerReady = true
		active.mu.Unlock()
		active.readyOnce.Do(func() { close(active.readyDone) })
	}
}

func (e *Endpoint) sendChannel(ctx context.Context, active *session, message channel.MessageBase) error {
	for {
		if err := active.channel.WaitReady(ctx); err != nil {
			return err
		}
		err := active.channel.Send(message)
		if !errors.Is(err, channel.ErrLinkNotReady) {
			return err
		}
		// Another concurrent sender may consume the available TX-window slot
		// between WaitReady and Send. Channel owns serialization; retry the
		// readiness check instead of adding a second session-wide mutex.
	}
}

func (e *Endpoint) completeAuthentication(active *session, err error) {
	active.authOnce.Do(func() {
		active.mu.Lock()
		active.authErr = err
		active.authenticated = err == nil
		active.mu.Unlock()
		close(active.authDone)
		if err == nil {
			e.notifyAuthenticated(active)
			ctx, cancel := context.WithTimeout(context.Background(), e.networkWait)
			readyErr := e.sendChannel(ctx, active, &authMessage{kind: authKindReady})
			cancel()
			if readyErr != nil {
				e.reportPeerError(fmt.Errorf("send authentication ready: %w", readyErr))
				active.link.Teardown()
				return
			}
		}
	})
}

func (e *Endpoint) notifyAuthenticated(active *session) {
	active.mu.RLock()
	sender := bytes.Clone(active.sender)
	active.mu.RUnlock()
	peer, err := core.NewPeerID(sender)
	if err != nil {
		return
	}
	e.mu.Lock()
	observer := e.observer
	e.mu.Unlock()
	if observer != nil {
		e.reportPeerError(observer.Authenticated(peer))
	}
}

func (e *Endpoint) waitAuthenticated(ctx context.Context, active *session) error {
	select {
	case <-active.authDone:
		active.mu.RLock()
		err := active.authErr
		authenticated := active.authenticated
		active.mu.RUnlock()
		if err != nil {
			return err
		}
		if !authenticated {
			return ErrRealmAuthentication
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-active.readyDone:
		active.mu.RLock()
		ready := active.peerReady
		active.mu.RUnlock()
		if !ready {
			return ErrRealmAuthentication
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
