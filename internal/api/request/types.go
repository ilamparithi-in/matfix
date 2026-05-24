package request

// # Send

// SendRequest is the JSON body of POST /v1/send.
type SendRequest struct {
	AccountID      string         `json:"account_id"`
	Destination    string         `json:"destination"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Message        MessagePayload `json:"message"`
}

// # Ask

// AskRequest is the JSON body of POST /v1/ask.
type AskRequest struct {
	AccountID      string         `json:"account_id"`
	Destination    string         `json:"destination"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Message        MessagePayload `json:"message"`
	Filter         FilterSpec     `json:"filter"`
	TimeoutSeconds int            `json:"timeout_seconds"`
}

// # Receive

// ReceiveRequest is the JSON body of POST /v1/receive.
type ReceiveRequest struct {
	AccountID      string     `json:"account_id"`
	Filter         FilterSpec `json:"filter"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	Limit          int        `json:"limit,omitempty"`
}

// # Message payload

// MessagePayload is a discriminated union of outbound message types.
// The Type field selects which other fields apply.
//
// Supported types: "text", "html", "reply", "reaction", "edit", "redaction".
type MessagePayload struct {
	// Type selects the message variant.
	Type string `json:"type"`

	// Body is the plain-text message body. Used by: text, html, reply, edit.
	Body string `json:"body,omitempty"`

	// FormattedBody is the HTML-formatted body. Used by: html, reply, edit.
	FormattedBody string `json:"formatted_body,omitempty"`

	// InReplyTo is the event ID this message replies to. Used by: reply.
	InReplyTo string `json:"in_reply_to,omitempty"`

	// TargetEventID is the event ID being reacted to, edited, or redacted.
	// Used by: reaction, edit, redaction.
	TargetEventID string `json:"target_event_id,omitempty"`

	// Key is the reaction emoji or text. Used by: reaction.
	Key string `json:"key,omitempty"`

	// Reason is an optional redaction reason. Used by: redaction.
	Reason string `json:"reason,omitempty"`
}

// # Filter spec

// FilterSpec describes the matching criteria for inbound events in
// receive and ask requests.
type FilterSpec struct {
	// RoomID restricts matches to a specific room. Empty = any room.
	RoomID string `json:"room_id,omitempty"`

	// SenderID restricts matches to a specific sender. Empty = any sender.
	SenderID string `json:"sender_id,omitempty"`

	// EventType restricts the inbound bus event type (e.g. "inbound.message").
	// Empty = inbound.message (default).
	EventType string `json:"event_type,omitempty"`

	// InReplyTo restricts matches to events whose in_reply_to equals this event ID.
	InReplyTo string `json:"in_reply_to,omitempty"`

	// BodyRegex is an optional regular expression matched against the event body.
	BodyRegex string `json:"body_regex,omitempty"`
}
