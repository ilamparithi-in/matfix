package submission

import "github.com/ilamparithi-in/matfix/internal/engine"

// # Request types

// SubmitRequest is the input to SubmissionManager.Submit.
type SubmitRequest struct {
	// AccountID selects which Matrix account sends the message.
	AccountID string

	// Destination is a Matrix room ID (!roomid:server), room alias (#alias:server),
	// or user ID (@user:server) to direct-message.
	Destination string

	// Message is the outbound event payload.
	Message engine.SendRequest

	// IdempotencyKey is an optional caller-supplied deduplication key.
	// When non-empty and a job with the same key already exists for AccountID,
	// Submit returns the existing job ID without inserting a duplicate.
	IdempotencyKey string
}
