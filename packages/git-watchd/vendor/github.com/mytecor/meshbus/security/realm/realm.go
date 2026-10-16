package realm

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

const (
	KeySize   = 32
	NonceSize = 32
	ProofSize = sha256.Size
)

const (
	defaultIDDomain             = "meshbus-realm-id-v1"
	defaultAuthenticationDomain = "meshbus-realm-auth-v1"
)

var (
	ErrInvalidConfig = errors.New("invalid realm configuration")
	ErrInvalidKey    = errors.New("invalid realm key")
	ErrInvalidProof  = errors.New("invalid realm proof input")
)

// Config defines the cryptographic domains for one realm. Empty domains use
// the meshbus defaults. Applications migrating an existing wire protocol may
// set explicit domains to preserve its identifiers and proofs.
type Config struct {
	Key                  []byte
	IDDomain             string
	AuthenticationDomain string
}

// Realm is an immutable shared-secret membership boundary.
type Realm struct {
	key                  [KeySize]byte
	id                   [sha256.Size]byte
	authenticationDomain string
}

// GenerateKey returns fresh 256-bit realm key material.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate realm key: %w", err)
	}
	return key, nil
}

// Open validates the key and derives the realm's public identifier.
func Open(config Config) (*Realm, error) {
	if len(config.Key) != KeySize {
		return nil, fmt.Errorf("%w: %w: expected %d bytes", ErrInvalidConfig, ErrInvalidKey, KeySize)
	}
	if config.IDDomain == "" {
		config.IDDomain = defaultIDDomain
	}
	if config.AuthenticationDomain == "" {
		config.AuthenticationDomain = defaultAuthenticationDomain
	}

	opened := &Realm{authenticationDomain: config.AuthenticationDomain}
	copy(opened.key[:], config.Key)
	digest := sha256.New()
	_, _ = digest.Write([]byte(config.IDDomain))
	_, _ = digest.Write(opened.key[:])
	copy(opened.id[:], digest.Sum(nil))
	return opened, nil
}

// ID returns a copy of the public, domain-separated realm identifier.
func (r *Realm) ID() []byte {
	if r == nil {
		return nil
	}
	return bytes.Clone(r.id[:])
}

// Proof binds a fresh nonce and the ordered, transport-authenticated peer
// identities to possession of the realm key.
func (r *Realm) Proof(nonce, challenger, responder []byte) ([]byte, error) {
	if r == nil || len(nonce) != NonceSize || len(challenger) == 0 || len(challenger) != len(responder) {
		return nil, fmt.Errorf("%w: require a %d-byte nonce and equal non-empty peer identities", ErrInvalidProof, NonceSize)
	}
	mac := hmac.New(sha256.New, r.key[:])
	_, _ = mac.Write([]byte(r.authenticationDomain))
	_, _ = mac.Write(nonce)
	_, _ = mac.Write(challenger)
	_, _ = mac.Write(responder)
	return mac.Sum(nil), nil
}

// Verify reports whether proof is the expected membership proof for the
// ordered peer identities. Invalid proof inputs fail closed.
func (r *Realm) Verify(proof, nonce, challenger, responder []byte) bool {
	expected, err := r.Proof(nonce, challenger, responder)
	return err == nil && hmac.Equal(expected, proof)
}
