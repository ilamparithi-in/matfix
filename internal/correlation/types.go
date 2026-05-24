package correlation

import (
	"context"
	"time"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// # Filter spec

// FilterSpec defines the matching criteria for inbound events.
type FilterSpec struct {
	// RoomID restricts matches to a specific room. Empty = any room.
	RoomID string

	// SenderID restricts matches to a specific sender. Empty = any sender.
	SenderID string

	// EventType restricts the inbound bus event type.
	// Zero value defaults to bus.EventInboundMessage during matching.
	EventType bus.EventType

	// InReplyTo restricts matches to events whose InReplyTo field equals this
	// event ID. Empty = no relation filter.
	InReplyTo string

	// BodyRegex is an optional regular expression matched against the event body.
	// Empty = no body filter.
	BodyRegex string
}

// # Requests

// AskRequest is the input to CorrelationManager.RegisterAsk.
type AskRequest struct {
	// CorrelationID is a caller-supplied opaque identifier (typically the outbound job ID).
	// A UUID is generated when empty.
	CorrelationID string

	// AccountID selects which account's inbound stream to watch.
	AccountID string

	// OutboundEventID is the Matrix event ID the relay sent. When Filter.InReplyTo
	// is empty, OutboundEventID is used as the default InReplyTo filter, implementing
	// the preferred m.in_reply_to correlation strategy.
	OutboundEventID string

	// Filter defines the criteria an inbound event must satisfy to resolve this ask.
	Filter FilterSpec

	// Timeout is how long to wait for a matching event before the handle expires.
	Timeout time.Duration
}

// ReceiveRequest is the input to CorrelationManager.RegisterReceive.
type ReceiveRequest struct {
	// CorrelationID is a caller-supplied opaque identifier.
	// A UUID is generated when empty.
	CorrelationID string

	// AccountID selects which account's inbound stream to watch.
	AccountID string

	// Filter defines the criteria an inbound event must satisfy to be collected.
	Filter FilterSpec

	// Timeout is the maximum duration to collect events.
	Timeout time.Duration

	// Limit is the maximum number of events to collect before resolving early.
	// 0 means unlimited - the window closes on Timeout only.
	Limit int
}

// # Handles

// AskHandle is returned by RegisterAsk. Callers block on Wait until a matching
// event arrives or the timeout elapses.
type AskHandle struct {
	// ID is the correlation ID assigned to this ask.
	ID     string
	result chan bus.EventEnvelope
	done   chan struct{}
}

// Wait blocks until a matching event arrives, the timeout elapses, or ctx is
// cancelled. Returns (nil, nil) when the ask times out without a match.
func (h *AskHandle) Wait(ctx context.Context) (*bus.EventEnvelope, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case env, ok := <-h.result:
		if !ok {
			return nil, nil // timed out or cancelled
		}
		return &env, nil
	}
}

// Done returns a channel closed when the handle is resolved or cancelled.
func (h *AskHandle) Done() <-chan struct{} { return h.done }

// ReceiveHandle is returned by RegisterReceive. Callers block on Wait until
// the receive window closes (timeout or event limit reached).
type ReceiveHandle struct {
	// ID is the correlation ID assigned to this receive subscription.
	ID     string
	result chan []bus.EventEnvelope
	done   chan struct{}
}

// Wait blocks until the receive window closes or ctx is cancelled.
// Returns (nil, nil) when the window closed via the internal done channel.
func (h *ReceiveHandle) Wait(ctx context.Context) ([]bus.EventEnvelope, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case events, ok := <-h.result:
		if !ok {
			return nil, nil
		}
		return events, nil
	}
}

// Done returns a channel closed when the handle is resolved or cancelled.
func (h *ReceiveHandle) Done() <-chan struct{} { return h.done }
