package rns

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Quad4-Software/Reticulum-Go/pkg/identity"
	"github.com/Quad4-Software/Reticulum-Go/pkg/rnsutil"
)

const privateIdentitySize = 64

func isInlineIdentitySource(source string) bool {
	if _, err := os.Stat(source); err == nil || !errors.Is(err, os.ErrNotExist) {
		return false
	}
	_, recognized := privateIdentityEncoding(source)
	return recognized
}

func loadOrCreateIdentity(source string) (*identity.Identity, error) {
	loaded, err := rnsutil.LoadIdentity(source)
	if err == nil {
		return loaded, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	loaded, recognized, importErr := importPrivateIdentity(source)
	if recognized {
		return loaded, importErr
	}

	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		return nil, err
	}
	return rnsutil.GenerateIdentity(source)
}

// importPrivateIdentity delegates decoding and validation to Reticulum-Go.
// The bool distinguishes an encoded candidate from an ordinary path.
func importPrivateIdentity(source string) (*identity.Identity, bool, error) {
	encoding, recognized := privateIdentityEncoding(source)
	if !recognized {
		return nil, false, nil
	}
	loaded, err := rnsutil.ImportPrivateIdentity(strings.TrimSpace(source), encoding)
	if err != nil {
		return nil, true, fmt.Errorf("invalid encoded private RNS identity: %w", err)
	}
	return loaded, true, nil
}

func privateIdentityEncoding(source string) (rnsutil.Encoding, bool) {
	value := strings.TrimSpace(source)
	for _, encoding := range []rnsutil.Encoding{rnsutil.EncHex, rnsutil.EncBase64, rnsutil.EncBase32} {
		decoded, err := rnsutil.DecodeBytes(value, encoding)
		valid := err == nil && len(decoded) == privateIdentitySize
		for index := range decoded {
			decoded[index] = 0
		}
		if valid {
			return encoding, true
		}
	}
	return 0, false
}
