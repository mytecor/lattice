package rns

import (
	"fmt"

	"github.com/mytecor/meshbus/security/realm"
)

const (
	authMessageType   uint16 = 0x0100
	directMessageType uint16 = 0x0101
	authNonceSize            = realm.NonceSize
	authProofSize            = realm.ProofSize
	authKindChallenge byte   = 1
	authKindResponse  byte   = 2
	authKindReady     byte   = 3
)

type authMessage struct {
	kind  byte
	nonce []byte
	proof []byte
}

func (m *authMessage) Pack() ([]byte, error) {
	if m.kind == authKindReady {
		if len(m.nonce) != 0 || len(m.proof) != 0 {
			return nil, fmt.Errorf("ready must not contain authentication data")
		}
		return []byte{authKindReady}, nil
	}
	if len(m.nonce) != authNonceSize {
		return nil, fmt.Errorf("invalid authentication nonce")
	}
	switch m.kind {
	case authKindChallenge:
		if len(m.proof) != 0 {
			return nil, fmt.Errorf("challenge must not contain a proof")
		}
	case authKindResponse:
		if len(m.proof) != authProofSize {
			return nil, fmt.Errorf("invalid authentication proof")
		}
	default:
		return nil, fmt.Errorf("invalid authentication message kind")
	}
	data := make([]byte, 1, 1+authNonceSize+len(m.proof))
	data[0] = m.kind
	data = append(data, m.nonce...)
	data = append(data, m.proof...)
	return data, nil
}

func (m *authMessage) Unpack(data []byte) error {
	if len(data) == 1 && data[0] == authKindReady {
		m.kind = authKindReady
		m.nonce = m.nonce[:0]
		m.proof = m.proof[:0]
		return nil
	}
	if len(data) != 1+authNonceSize && len(data) != 1+authNonceSize+authProofSize {
		return fmt.Errorf("invalid authentication message length")
	}
	m.kind = data[0]
	m.nonce = append(m.nonce[:0], data[1:1+authNonceSize]...)
	m.proof = append(m.proof[:0], data[1+authNonceSize:]...)
	_, err := m.Pack()
	return err
}

func (m *authMessage) GetType() uint16 { return authMessageType }

type directMessage struct {
	data []byte
}

func (m *directMessage) Pack() ([]byte, error) {
	return m.data, nil
}

func (m *directMessage) Unpack(data []byte) error {
	m.data = append(m.data[:0], data...)
	return nil
}

func (m *directMessage) GetType() uint16 {
	return directMessageType
}
