package core

import (
	"time"

	"github.com/mytecor/meshbus/internal/protocol/wire"
)

var eventMagic = wire.EventMagic

func isEventMessage(payload []byte) bool {
	return wire.IsEvent(payload)
}

func encodeEvent(event Event, maxPayload int, maxTTL time.Duration) ([]byte, error) {
	return wire.EncodeEvent(wire.Event{
		ID: [16]byte(event.ID), Topic: event.Topic, PublishedAt: event.PublishedAt,
		TTL: event.TTL, ContentType: event.ContentType, Payload: event.Payload,
	}, maxPayload, maxTTL)
}

func decodeEvent(data []byte, maxPayload int, maxTTL time.Duration) (Event, error) {
	event, err := wire.DecodeEvent(data, maxPayload, maxTTL)
	if err != nil {
		return Event{}, err
	}
	return Event{
		ID: EventID(event.ID), Topic: event.Topic, PublishedAt: event.PublishedAt,
		TTL: event.TTL, ContentType: event.ContentType, Payload: event.Payload,
	}, nil
}
