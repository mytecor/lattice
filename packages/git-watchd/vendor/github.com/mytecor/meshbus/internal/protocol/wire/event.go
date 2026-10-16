// Package wire owns the versioned binary protocol used by meshbus control and
// event frames. It deliberately has no dependency on the public orchestration
// package.
package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/mytecor/meshbus/internal/protocol/subject"
)

var (
	EventMagic      = [4]byte{'M', 'B', 'E', 1}
	ErrInvalidFrame = errors.New("invalid event")
)

const EventHeaderSize = 4 + 16 + 8 + 4 + 2 + 2 + 4

type Event struct {
	ID          [16]byte
	Topic       string
	PublishedAt time.Time
	TTL         time.Duration
	ContentType string
	Payload     []byte
}

func IsEvent(payload []byte) bool {
	return len(payload) >= len(EventMagic) && bytes.Equal(payload[:len(EventMagic)], EventMagic[:])
}

func EncodeEvent(event Event, maxPayload int, maxTTL time.Duration) ([]byte, error) {
	if event.ID == [16]byte{} || subject.ValidateTopic(event.Topic) != nil || event.PublishedAt.IsZero() ||
		event.TTL < time.Millisecond || event.TTL > maxTTL || len(event.Payload) > maxPayload ||
		len(event.ContentType) > 128 {
		return nil, ErrInvalidFrame
	}
	for _, character := range []byte(event.ContentType) {
		if character < 0x20 || character > 0x7e {
			return nil, ErrInvalidFrame
		}
	}
	ttlMillis := event.TTL.Milliseconds()
	if ttlMillis <= 0 || ttlMillis > int64(^uint32(0)) {
		return nil, ErrInvalidFrame
	}

	data := make([]byte, EventHeaderSize, EventHeaderSize+len(event.Topic)+len(event.ContentType)+len(event.Payload))
	copy(data[:4], EventMagic[:])
	copy(data[4:20], event.ID[:])
	binary.BigEndian.PutUint64(data[20:28], uint64(event.PublishedAt.UnixMilli()))
	binary.BigEndian.PutUint32(data[28:32], uint32(ttlMillis))
	binary.BigEndian.PutUint16(data[32:34], uint16(len(event.Topic)))
	binary.BigEndian.PutUint16(data[34:36], uint16(len(event.ContentType)))
	binary.BigEndian.PutUint32(data[36:40], uint32(len(event.Payload)))
	data = append(data, event.Topic...)
	data = append(data, event.ContentType...)
	data = append(data, event.Payload...)
	return data, nil
}

func DecodeEvent(data []byte, maxPayload int, maxTTL time.Duration) (Event, error) {
	if !IsEvent(data) || len(data) < EventHeaderSize {
		return Event{}, ErrInvalidFrame
	}
	var id [16]byte
	copy(id[:], data[4:20])
	topicLength := uint64(binary.BigEndian.Uint16(data[32:34]))
	contentTypeLength := uint64(binary.BigEndian.Uint16(data[34:36]))
	payloadLength := uint64(binary.BigEndian.Uint32(data[36:40]))
	wantLength := uint64(EventHeaderSize) + topicLength + contentTypeLength + payloadLength
	if wantLength != uint64(len(data)) || payloadLength > uint64(maxPayload) {
		return Event{}, ErrInvalidFrame
	}
	offset := EventHeaderSize
	event := Event{
		ID:          id,
		PublishedAt: time.UnixMilli(int64(binary.BigEndian.Uint64(data[20:28]))).UTC(),
		TTL:         time.Duration(binary.BigEndian.Uint32(data[28:32])) * time.Millisecond,
		Topic:       string(data[offset : offset+int(topicLength)]),
	}
	offset += int(topicLength)
	event.ContentType = string(data[offset : offset+int(contentTypeLength)])
	offset += int(contentTypeLength)
	event.Payload = bytes.Clone(data[offset:])
	if _, err := EncodeEvent(event, maxPayload, maxTTL); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrInvalidFrame, err)
	}
	return event, nil
}
