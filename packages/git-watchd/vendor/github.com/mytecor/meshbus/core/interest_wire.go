package core

import (
	"time"

	"github.com/mytecor/meshbus/internal/protocol/wire"
)

const (
	interestKindQuery   = wire.InterestQuery
	interestKindRenew   = wire.InterestRenew
	maxPatternsPerRenew = wire.MaxPatternsPerRenew
)

type interestMessage struct {
	kind     byte
	lease    time.Duration
	patterns []string
}

func isInterestMessage(payload []byte) bool {
	return wire.IsInterest(payload)
}

func encodeInterestQuery() []byte {
	return wire.EncodeInterestQuery()
}

func encodeInterestRenew(patterns []string, lease time.Duration) ([]byte, error) {
	return wire.EncodeInterestRenew(patterns, lease)
}

func decodeInterest(data []byte) (interestMessage, error) {
	message, err := wire.DecodeInterest(data)
	if err != nil {
		return interestMessage{}, err
	}
	return interestMessage{kind: message.Kind, lease: message.Lease, patterns: message.Patterns}, nil
}
