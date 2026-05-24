package bus

import "time"

// # Event type constants

// EventType identifies the kind of event carried in an EventEnvelope.
type EventType string

const (
	// Inbound events published by the Sync Manager.
	EventInboundMessage    EventType = "inbound.message"
	EventInboundReaction   EventType = "inbound.reaction"
	EventInboundEdit       EventType = "inbound.edit"
	EventInboundRedaction  EventType = "inbound.redaction"
	EventInboundMembership EventType = "inbound.membership"
	EventInboundReceipt    EventType = "inbound.receipt"

	// Delivery lifecycle events published by the Worker Pool.
	EventDeliveryAcknowledged EventType = "delivery.acknowledged"
	EventRetryScheduled       EventType = "delivery.retry_scheduled"
	EventRetryExhausted       EventType = "delivery.retry_exhausted"

	// Correlation events published by the Correlation Manager.
	EventAskRegistered EventType = "ask.registered"
	EventAskResolved   EventType = "ask.resolved"

	// Crypto events published by the Crypto Manager.
	EventEncryptionFailure EventType = "crypto.encryption_failure"
)

// # Envelope

// EventEnvelope is the common wrapper for all internal events.
// Consumers type-assert Payload to the concrete event struct for their EventType.
type EventEnvelope struct {
	Type      EventType
	AccountID string
	RoomID    string
	Payload   any
}

// # Inbound event payloads

// InboundMessageEvent carries a received m.room.message event.
type InboundMessageEvent struct {
	EventID       string
	SenderID      string
	Body          string
	FormattedBody string
	MsgType       string
	// InReplyTo holds the event ID this message is a reply to, if any.
	InReplyTo string
	Timestamp time.Time
}

// InboundReactionEvent carries a received m.reaction event.
type InboundReactionEvent struct {
	EventID          string
	SenderID         string
	RelatesToEventID string
	Key              string
	Timestamp        time.Time
}

// InboundEditEvent carries a received m.room.message with a replace relation.
type InboundEditEvent struct {
	EventID          string
	SenderID         string
	RelatesToEventID string
	NewBody          string
	NewFormattedBody string
	Timestamp        time.Time
}

// InboundRedactionEvent carries a received m.room.redaction event.
type InboundRedactionEvent struct {
	EventID   string
	SenderID  string
	Redacts   string
	Reason    string
	Timestamp time.Time
}

// InboundMembershipEvent carries a received m.room.member state event.
type InboundMembershipEvent struct {
	EventID        string
	UserID         string
	Membership     string
	PrevMembership string
	Timestamp      time.Time
}

// InboundReceiptEvent carries a received m.receipt event.
type InboundReceiptEvent struct {
	UserID      string
	EventID     string
	ReceiptType string
	Timestamp   time.Time
}

// # Delivery event payloads

// DeliveryAcknowledgedEvent is published when the homeserver confirms a sent event.
type DeliveryAcknowledgedEvent struct {
	JobID         string
	MatrixEventID string
	Timestamp     time.Time
}

// RetryScheduledEvent is published when a failed job is scheduled for retry.
type RetryScheduledEvent struct {
	JobID       string
	RetryCount  int
	ScheduledAt time.Time
}

// RetryExhaustedEvent is published when a job has exhausted all retry attempts.
type RetryExhaustedEvent struct {
	JobID      string
	RetryCount int
	LastError  string
}

// # Correlation event payloads

// AskRegisteredEvent is published when a new ask correlation is created.
type AskRegisteredEvent struct {
	CorrelationID   string
	OutboundEventID string
}

// AskResolvedEvent is published when an ask correlation matches or times out.
type AskResolvedEvent struct {
	CorrelationID  string
	MatchedEventID string
	TimedOut       bool
}

// # Crypto event payloads

// EncryptionFailureEvent is published when an encrypted event cannot be decrypted.
type EncryptionFailureEvent struct {
	EventID  string
	SenderID string
	ErrorMsg string
}
