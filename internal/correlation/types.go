package correlation

import (
	"context"
	"time"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// # Filter types

// StringSetFilter is an include/exclude set filter for a string field.
// Include is a whitelist (nil = unrestricted). Exclude is a blacklist (nil = no exclusions).
// Both may be set simultaneously; exclude takes precedence.
type StringSetFilter struct {
	Include []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}

// FilterNode is a recursive filter expression.
// All set fields within a node are ANDed. An empty node (all fields zero/nil) always matches.
type FilterNode struct {
	// Set filters
	SenderID  *StringSetFilter `json:"sender_id,omitempty" yaml:"sender_id,omitempty"`
	RoomID    *StringSetFilter `json:"room_id,omitempty" yaml:"room_id,omitempty"`
	EventType *StringSetFilter `json:"event_type,omitempty" yaml:"event_type,omitempty"`

	// Scalar predicates (zero value = unset/inactive)
	InReplyTo        string `json:"in_reply_to,omitempty" yaml:"in_reply_to,omitempty"`
	BodyRegex        string `json:"body_regex,omitempty" yaml:"body_regex,omitempty"`
	ReactionKey      string `json:"reaction_key,omitempty" yaml:"reaction_key,omitempty"`
	RelatesToEventID string `json:"relates_to_event_id,omitempty" yaml:"relates_to_event_id,omitempty"`
	HasAttachment    *bool  `json:"has_attachment,omitempty" yaml:"has_attachment,omitempty"`
	MinTimestamp     int64  `json:"min_timestamp,omitempty" yaml:"min_timestamp,omitempty"`
	MaxTimestamp     int64  `json:"max_timestamp,omitempty" yaml:"max_timestamp,omitempty"`

	// Combinators
	AllOf []*FilterNode `json:"all_of,omitempty" yaml:"all_of,omitempty"`
	AnyOf []*FilterNode `json:"any_of,omitempty" yaml:"any_of,omitempty"`
	Not   *FilterNode   `json:"not,omitempty" yaml:"not,omitempty"`
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

	// BotUserID is the Matrix user ID of the sending account. When non-empty, events
	// whose sender matches this ID are automatically excluded from matching, preventing
	// the bot's own echoed message from resolving the ask.
	BotUserID string

	// Filter defines the criteria an inbound event must satisfy to resolve this ask.
	Filter FilterNode

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
	Filter FilterNode

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
