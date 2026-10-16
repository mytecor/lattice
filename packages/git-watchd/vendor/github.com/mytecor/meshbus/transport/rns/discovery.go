package rns

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/Quad4-Software/Reticulum-Go/pkg/identity"
	"github.com/mytecor/meshbus/core"
)

func (e *Endpoint) announceLoop(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = e.destination.Announce(false, nil, nil)
		}
	}
}

// announceHandler registers on the local aspect and turns validated
// realm announces into meshbus presence records.
type announceHandler struct {
	endpoint *Endpoint
	aspect   string
}

func (h *announceHandler) AspectFilter() []string     { return []string{h.aspect} }
func (h *announceHandler) ReceivePathResponses() bool { return true }
func (h *announceHandler) ReceivedAnnounce(destinationHash []byte, announced any, appData []byte, hops uint8) error {
	key := hex.EncodeToString(destinationHash)
	for _, waiter := range h.endpoint.connections.takeWaiters(key) {
		close(waiter)
	}
	// The single presence parser confirms the realm before the peer becomes
	// visible. Forged or foreign presence never reaches the directory.
	metadata, err := h.endpoint.codec.Parse(appData, h.endpoint.realmID)
	if err != nil {
		return nil
	}
	announcedIdentity, ok := announced.(*identity.Identity)
	if !ok || announcedIdentity == nil {
		return nil
	}
	peerID, err := core.NewPeerID(announcedIdentity.Hash())
	if err != nil {
		return nil
	}
	peer := core.Peer{
		ID:       peerID,
		Metadata: metadata,
		Hops:     hops,
	}
	seen := h.endpoint.directory.Remember(peer)
	if seen != nil {
		return nil
	}
	h.endpoint.connections.rememberDestination(announcedIdentity.Hash(), destinationHash)
	h.endpoint.mu.Lock()
	observer := h.endpoint.observer
	h.endpoint.mu.Unlock()
	if observer != nil {
		h.endpoint.reportPeerError(observer.Discovered(peer))
	}
	return nil
}
