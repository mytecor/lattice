package rns

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Presence wire constants. The generic presence descriptor is versioned
// independently from any application protocol: it carries only what
// realm discovery needs, never events, subscriptions or application logic.
const (
	genericProtocolVersion = "meshbus.v1"
	maxPresenceBytes       = 256
	// maxPresenceKeys bounds the advisory metadata a peer may carry in one
	// announce so a hostile peer cannot grow memory without bound.
	maxPresenceKeys = 16
)

var (
	// ErrInvalidPresence is returned when announce app_data is malformed,
	// oversized, or describes a foreign realm.
	ErrInvalidPresence = errors.New("invalid meshbus presence descriptor")
	// ErrRealmMismatch reports an announce that names a different realm.
	ErrRealmMismatch = errors.New("announce names a different realm")
)

// Presence is the small bounded announcement descriptor used by new meshbus
// applications. It names the announcing peer's realm (the discovery security
// boundary) and carries bounded advisory metadata. It never carries events,
// subscriptions, or bulk data.
type Presence struct {
	// Protocol is the wire version marker ("meshbus.v1").
	Protocol string `json:"p"`
	// Realm is the hex-encoded realm identifier the announcing peer belongs to.
	Realm string `json:"realm"`
	// Metadata is an optional bounded advisory map (for example os/arch).
	Metadata map[string]string `json:"meta,omitempty"`
}

// marshal encodes the presence descriptor within the announce app-data budget.
func (p Presence) marshal() ([]byte, error) {
	if strings.TrimSpace(p.Protocol) == "" {
		p.Protocol = genericProtocolVersion
	}
	if p.Protocol != genericProtocolVersion {
		return nil, fmt.Errorf("%w: unsupported protocol %q", ErrInvalidPresence, p.Protocol)
	}
	realmID, err := hex.DecodeString(p.Realm)
	if err != nil || len(realmID) != 32 || p.Realm != strings.ToLower(p.Realm) {
		return nil, fmt.Errorf("%w: realm must be 32 lowercase hexadecimal bytes", ErrInvalidPresence)
	}
	if p.Metadata == nil {
		p.Metadata = map[string]string{}
	}
	if len(p.Metadata) > maxPresenceKeys {
		return nil, fmt.Errorf("%w: %d metadata keys exceed %d", ErrInvalidPresence, len(p.Metadata), maxPresenceKeys)
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPresence, err)
	}
	if len(data) > maxPresenceBytes {
		return nil, fmt.Errorf("%w: encoded size %d exceeds %d bytes", ErrInvalidPresence, len(data), maxPresenceBytes)
	}
	return data, nil
}

// MarshalBinary encodes a bounded meshbus.v1 presence descriptor.
func (p Presence) MarshalBinary() ([]byte, error) { return p.marshal() }

// parsePresence decodes and validates an inbound generic presence descriptor.
// It returns the presence together with its decoded realm id.
func parsePresence(data []byte) (Presence, []byte, error) {
	if len(data) == 0 || len(data) > maxPresenceBytes {
		return Presence{}, nil, fmt.Errorf("%w: encoded size must be between 1 and %d bytes", ErrInvalidPresence, maxPresenceBytes)
	}
	var presence Presence
	if err := json.Unmarshal(data, &presence); err != nil {
		return Presence{}, nil, fmt.Errorf("%w: %v", ErrInvalidPresence, err)
	}
	if presence.Protocol != genericProtocolVersion {
		return Presence{}, nil, fmt.Errorf("%w: unsupported protocol %q", ErrInvalidPresence, presence.Protocol)
	}
	if len(presence.Metadata) > maxPresenceKeys {
		return Presence{}, nil, fmt.Errorf("%w: %d metadata keys exceed %d", ErrInvalidPresence, len(presence.Metadata), maxPresenceKeys)
	}
	if presence.Metadata == nil {
		presence.Metadata = map[string]string{}
	}
	realmID, err := hex.DecodeString(presence.Realm)
	if err != nil || len(realmID) != 32 || presence.Realm != strings.ToLower(presence.Realm) {
		return Presence{}, nil, fmt.Errorf("%w: realm must be 32 lowercase hexadecimal bytes", ErrInvalidPresence)
	}
	return presence, realmID, nil
}

// ParsePresence validates and decodes a bounded meshbus.v1 presence
// descriptor. Realm matching remains the endpoint's responsibility.
func ParsePresence(data []byte) (Presence, error) {
	presence, _, err := parsePresence(data)
	return presence, err
}

// genericPresenceCodec is the single meshbus.v1 announce format.
type genericPresenceCodec struct {
	metadata map[string]string
	passive  bool
}

// Build encodes a generic presence descriptor announcing realmID with the
// given advisory metadata.
func (c genericPresenceCodec) Build(realmID, identityHash []byte) ([]byte, error) {
	if c.passive {
		return nil, nil
	}
	presence := Presence{
		Protocol: genericProtocolVersion,
		Realm:    hex.EncodeToString(realmID),
		Metadata: clonePresenceMetadata(c.metadata),
	}
	return presence.marshal()
}

// Parse decodes a generic presence descriptor and verifies it names the same
// realm the adapter is bound to. A mismatch is rejected before any delivery.
func (genericPresenceCodec) Parse(appData []byte, expectedRealmID []byte) (map[string]string, error) {
	presence, realmID, err := parsePresence(appData)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(realmID, expectedRealmID) {
		return nil, fmt.Errorf("%w: announced %s", ErrRealmMismatch, presence.Realm)
	}
	return clonePresenceMetadata(presence.Metadata), nil
}

func clonePresenceMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
