package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/mytecor/meshbus/internal/protocol/subject"
)

var InterestMagic = [4]byte{'M', 'B', 'I', 1}

const (
	InterestQuery       byte = 1
	InterestRenew       byte = 2
	InterestRenewHeader      = 4 + 1 + 4 + 2
	MaxPatternsPerRenew      = int(^uint16(0))
)

type Interest struct {
	Kind     byte
	Lease    time.Duration
	Patterns []string
}

func IsInterest(payload []byte) bool {
	return len(payload) >= len(InterestMagic) && bytes.Equal(payload[:len(InterestMagic)], InterestMagic[:])
}

func EncodeInterestQuery() []byte {
	data := make([]byte, 5)
	copy(data, InterestMagic[:])
	data[4] = InterestQuery
	return data
}

func EncodeInterestRenew(patterns []string, lease time.Duration) ([]byte, error) {
	if len(patterns) == 0 || len(patterns) > MaxPatternsPerRenew || lease < time.Millisecond ||
		lease > time.Duration(^uint32(0))*time.Millisecond {
		return nil, ErrInvalidFrame
	}
	size := InterestRenewHeader
	for _, pattern := range patterns {
		if err := subject.ValidatePattern(pattern); err != nil {
			return nil, err
		}
		size += 2 + len(pattern)
	}
	data := make([]byte, InterestRenewHeader, size)
	copy(data, InterestMagic[:])
	data[4] = InterestRenew
	binary.BigEndian.PutUint32(data[5:9], uint32(lease.Milliseconds()))
	binary.BigEndian.PutUint16(data[9:11], uint16(len(patterns)))
	for _, pattern := range patterns {
		var length [2]byte
		binary.BigEndian.PutUint16(length[:], uint16(len(pattern)))
		data = append(data, length[:]...)
		data = append(data, pattern...)
	}
	return data, nil
}

func DecodeInterest(data []byte) (Interest, error) {
	if !IsInterest(data) || len(data) < 5 {
		return Interest{}, ErrInvalidFrame
	}
	switch data[4] {
	case InterestQuery:
		if len(data) != 5 {
			return Interest{}, ErrInvalidFrame
		}
		return Interest{Kind: InterestQuery}, nil
	case InterestRenew:
		if len(data) < InterestRenewHeader {
			return Interest{}, ErrInvalidFrame
		}
		lease := time.Duration(binary.BigEndian.Uint32(data[5:9])) * time.Millisecond
		count := int(binary.BigEndian.Uint16(data[9:11]))
		if lease < time.Millisecond || count == 0 {
			return Interest{}, ErrInvalidFrame
		}
		patterns := make([]string, 0, count)
		offset := InterestRenewHeader
		for range count {
			if offset+2 > len(data) {
				return Interest{}, ErrInvalidFrame
			}
			length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if length == 0 || offset+length > len(data) {
				return Interest{}, ErrInvalidFrame
			}
			pattern := string(data[offset : offset+length])
			offset += length
			if err := subject.ValidatePattern(pattern); err != nil {
				return Interest{}, fmt.Errorf("%w: %v", ErrInvalidFrame, err)
			}
			patterns = append(patterns, pattern)
		}
		if offset != len(data) {
			return Interest{}, ErrInvalidFrame
		}
		return Interest{Kind: InterestRenew, Lease: lease, Patterns: patterns}, nil
	default:
		return Interest{}, ErrInvalidFrame
	}
}
