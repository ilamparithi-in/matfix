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
	Filter         FilterNode     `json:"filter"`
	TimeoutSeconds int            `json:"timeout_seconds"`
}

// # Receive

// ReceiveRequest is the JSON body of POST /v1/receive.
type ReceiveRequest struct {
	AccountID      string     `json:"account_id"`
	Filter         FilterNode `json:"filter"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	Limit          int        `json:"limit,omitempty"`
}

// # Message payload

// MessagePayload is a discriminated union of outbound message types.
// The Type field selects which other fields apply.
//
// Supported types: "text", "html", "reply", "reaction", "edit", "redaction", "file".
type MessagePayload struct {
	// Type selects the message variant.
	Type string `json:"type"`

	// Body is the plain-text message body. Used by: text, html, reply, edit,
	// and as the media caption for file.
	Body string `json:"body,omitempty"`

	// FormattedBody is the HTML-formatted body. Used by: html, reply, edit,
	// and as the formatted media caption for file.
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

	// File is the attachment payload. Used by: file.
	File *FileAttachment `json:"file,omitempty"`
}

// FileAttachment carries the base64-encoded file bytes and optional media
// metadata for a "file" message payload.
type FileAttachment struct {
	// Data is the base64-encoded file content (standard encoding, no line breaks).
	Data string `json:"data"`

	// MimeType is the MIME type of the file (e.g. "image/png", "application/pdf").
	MimeType string `json:"mime_type"`

	// Filename is the display name for the attachment.
	Filename string `json:"filename"`

	// Width is the pixel width for images and videos.
	Width int `json:"width,omitempty"`

	// Height is the pixel height for images and videos.
	Height int `json:"height,omitempty"`

	// Duration is the playback duration in milliseconds for audio and video.
	Duration int `json:"duration,omitempty"`
}

// # Filter types

// StringSetFilter is an include/exclude set filter for a string field.
type StringSetFilter struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// FilterNode is a recursive filter expression.
// All set fields within a node are ANDed. An empty node (all fields zero/nil) always matches.
type FilterNode struct {
	SenderID  *StringSetFilter `json:"sender_id,omitempty"`
	RoomID    *StringSetFilter `json:"room_id,omitempty"`
	EventType *StringSetFilter `json:"event_type,omitempty"`

	InReplyTo        string `json:"in_reply_to,omitempty"`
	BodyRegex        string `json:"body_regex,omitempty"`
	ReactionKey      string `json:"reaction_key,omitempty"`
	RelatesToEventID string `json:"relates_to_event_id,omitempty"`
	HasAttachment    *bool  `json:"has_attachment,omitempty"`
	MinTimestamp     int64  `json:"min_timestamp,omitempty"`
	MaxTimestamp     int64  `json:"max_timestamp,omitempty"`

	AllOf []*FilterNode `json:"all_of,omitempty"`
	AnyOf []*FilterNode `json:"any_of,omitempty"`
	Not   *FilterNode   `json:"not,omitempty"`
}
