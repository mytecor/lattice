package rns

import (
	"bytes"

	"github.com/Quad4-Software/Reticulum-Go/pkg/channel"
	"github.com/Quad4-Software/Reticulum-Go/pkg/identity"
	"github.com/Quad4-Software/Reticulum-Go/pkg/link"
	"github.com/mytecor/meshbus/core"
)

func (e *Endpoint) acceptLink(value any) {
	inbound, ok := value.(*link.Link)
	if !ok || inbound == nil {
		return
	}
	active, err := e.newSession(inbound, nil, nil)
	if err != nil {
		inbound.Teardown()
		return
	}
	authenticate := func(remote *identity.Identity) {
		if remote == nil {
			return
		}
		active.mu.Lock()
		active.sender = bytes.Clone(remote.Hash())
		pendingAuth := active.pendingAuth
		active.pendingAuth = make(map[byte]authMessage)
		active.mu.Unlock()
		e.connections.cacheSession(active, remote.Hash(), nil)
		e.beginAuthentication(active)
		for _, kind := range []byte{authKindChallenge, authKindResponse, authKindReady} {
			if message, exists := pendingAuth[kind]; exists {
				message := message
				go e.handleAuthentication(active, &message)
			}
		}
	}
	inbound.SetRemoteIdentifiedCallback(func(_ *link.Link, remote *identity.Identity) { authenticate(remote) })
	authenticate(inbound.GetRemoteIdentity())
}

func (e *Endpoint) newSession(rnsLink *link.Link, sender, destinationHash []byte) (*session, error) {
	rnsChannel := rnsLink.GetChannel()
	if err := rnsChannel.RegisterMessageType(authMessageType, func() channel.MessageBase { return &authMessage{} }); err != nil {
		return nil, err
	}
	if err := rnsChannel.RegisterMessageType(directMessageType, func() channel.MessageBase { return &directMessage{} }); err != nil {
		return nil, err
	}
	active := &session{
		link: rnsLink, channel: rnsChannel,
		sender: bytes.Clone(sender), pendingAuth: make(map[byte]authMessage),
		authDone: make(chan struct{}), readyDone: make(chan struct{}),
	}
	if err := rnsLink.SetResourceStrategy(link.AcceptApp); err != nil {
		return nil, err
	}
	rnsLink.SetResourceCallback(func(any) bool {
		active.mu.RLock()
		defer active.mu.RUnlock()
		return active.authenticated && active.authErr == nil && len(active.sender) != 0
	})
	rnsLink.SetResourceConcludedCallback(func(value any) {
		var data []byte
		switch incoming := value.(type) {
		case []byte:
			data = incoming
		case link.IncomingResource:
			data = incoming.Data
		default:
			return
		}
		go e.deliver(active, data)
	})
	rnsChannel.AddMessageHandler(func(message channel.MessageBase) bool {
		if authentication, ok := message.(*authMessage); ok {
			go e.handleAuthentication(active, authentication)
			return true
		}
		wire, ok := message.(*directMessage)
		if !ok {
			return false
		}
		go e.deliver(active, wire.data)
		return true
	})
	e.connections.cacheSession(active, sender, destinationHash)
	e.connections.rememberDestination(sender, destinationHash)
	return active, nil
}

func (e *Endpoint) deliver(active *session, data []byte) {
	active.mu.Lock()
	sender := bytes.Clone(active.sender)
	if len(sender) == 0 || !active.authenticated {
		active.mu.Unlock()
		return
	}
	active.mu.Unlock()
	message, err := core.NewReceivedMessage(sender, data)
	if err != nil {
		return
	}
	_ = e.handler(e.handlerContext(), message)
}
