package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"maunium.net/go/mautrix/id"

	"github.com/ilamparithi-in/matfix/internal/engine"
)

// # State

// State represents the delivery state of an outbound job.
type State string

const (
	StateAccepted     State = "accepted"
	StateQueued       State = "queued"
	StateSending      State = "sending"
	StateSent         State = "sent"
	StateAcknowledged State = "acknowledged"
	StateFailed       State = "failed"
	StateDeadLetter   State = "dead_letter"
)

// # Payload encoding

// payloadType is the discriminator tag written to the database.
type payloadType string

const (
	payloadTextMessage payloadType = "text_message"
	payloadHTMLMessage payloadType = "html_message"
	payloadReply       payloadType = "reply"
	payloadReaction    payloadType = "reaction"
	payloadEdit        payloadType = "edit"
	payloadRedaction   payloadType = "redaction"
	payloadFileMessage payloadType = "file_message"
)

// encodedPayload is the on-disk JSON representation of a send request.
type encodedPayload struct {
	Type    payloadType     `json:"type"`
	Content json.RawMessage `json:"content"`
}

// encodePayload serializes req to a JSON string for storage in the database.
func encodePayload(req engine.SendRequest) (string, error) {
	var pt payloadType
	switch req.(type) {
	case engine.TextMessage:
		pt = payloadTextMessage
	case engine.HTMLMessage:
		pt = payloadHTMLMessage
	case engine.Reply:
		pt = payloadReply
	case engine.Reaction:
		pt = payloadReaction
	case engine.Edit:
		pt = payloadEdit
	case engine.Redaction:
		pt = payloadRedaction
	case engine.FileMessage:
		pt = payloadFileMessage
	default:
		return "", fmt.Errorf("queue: unknown send request type %T", req)
	}

	content, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("queue: marshal payload content: %w", err)
	}
	b, err := json.Marshal(encodedPayload{Type: pt, Content: content})
	if err != nil {
		return "", fmt.Errorf("queue: marshal payload envelope: %w", err)
	}
	return string(b), nil
}

// decodePayload deserializes a database JSON string back to an engine.SendRequest.
func decodePayload(raw string) (engine.SendRequest, error) {
	var ep encodedPayload
	if err := json.Unmarshal([]byte(raw), &ep); err != nil {
		return nil, fmt.Errorf("queue: unmarshal payload: %w", err)
	}
	switch ep.Type {
	case payloadTextMessage:
		var v engine.TextMessage
		if err := json.Unmarshal(ep.Content, &v); err != nil {
			return nil, fmt.Errorf("queue: decode text_message: %w", err)
		}
		return v, nil
	case payloadHTMLMessage:
		var v engine.HTMLMessage
		if err := json.Unmarshal(ep.Content, &v); err != nil {
			return nil, fmt.Errorf("queue: decode html_message: %w", err)
		}
		return v, nil
	case payloadReply:
		var v engine.Reply
		if err := json.Unmarshal(ep.Content, &v); err != nil {
			return nil, fmt.Errorf("queue: decode reply: %w", err)
		}
		return v, nil
	case payloadReaction:
		var v engine.Reaction
		if err := json.Unmarshal(ep.Content, &v); err != nil {
			return nil, fmt.Errorf("queue: decode reaction: %w", err)
		}
		return v, nil
	case payloadEdit:
		var v engine.Edit
		if err := json.Unmarshal(ep.Content, &v); err != nil {
			return nil, fmt.Errorf("queue: decode edit: %w", err)
		}
		return v, nil
	case payloadRedaction:
		var v engine.Redaction
		if err := json.Unmarshal(ep.Content, &v); err != nil {
			return nil, fmt.Errorf("queue: decode redaction: %w", err)
		}
		return v, nil
	case payloadFileMessage:
		var v engine.FileMessage
		if err := json.Unmarshal(ep.Content, &v); err != nil {
			return nil, fmt.Errorf("queue: decode file_message: %w", err)
		}
		return v, nil
	default:
		return nil, fmt.Errorf("queue: unknown payload type %q", ep.Type)
	}
}

// # Job

// Job is the in-memory representation of an outbound message job.
type Job struct {
	ID             string
	AccountID      string
	RoomID         id.RoomID
	Request        engine.SendRequest
	State          State
	RetryCount     int
	ScheduledAt    time.Time
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
