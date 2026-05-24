package engine

import "maunium.net/go/mautrix/id"

// # Send request types

// SendRequest is implemented by all outbound message request types.
// The unexported marker method restricts the interface to this package.
type SendRequest interface{ isSendRequest() }

// TextMessage sends a plain-text m.room.message (msgtype m.text).
type TextMessage struct {
	Body string
}

// HTMLMessage sends a formatted m.room.message with an HTML body.
// FormattedBody is sanitized before transmission.
type HTMLMessage struct {
	Body          string
	FormattedBody string
}

// Reply sends a plain or formatted message that references InReplyTo as its
// Matrix reply relation (m.in_reply_to).
type Reply struct {
	InReplyTo     id.EventID
	Body          string
	FormattedBody string // optional; sanitized if non-empty
}

// Reaction sends an m.reaction event annotating TargetEventID with Key.
type Reaction struct {
	TargetEventID id.EventID
	Key           string
}

// Edit sends a replacement event (m.replace relation) updating TargetEventID.
// NewFormattedBody is optional; sanitized if non-empty.
type Edit struct {
	TargetEventID    id.EventID
	NewBody          string
	NewFormattedBody string
}

// Redaction sends a redaction event targeting TargetEventID.
type Redaction struct {
	TargetEventID id.EventID
	Reason        string
}

func (TextMessage) isSendRequest() {}
func (HTMLMessage) isSendRequest() {}
func (Reply) isSendRequest()       {}
func (Reaction) isSendRequest()    {}
func (Edit) isSendRequest()        {}
func (Redaction) isSendRequest()   {}
