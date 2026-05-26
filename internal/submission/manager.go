package submission

import (
	"context"
	"fmt"

	"github.com/ilamparithi-in/matfix/internal/config"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/queue"
)

// # ClientLookup

// ClientLookup returns the engine.Client for the given accountID.
// Returns false when the account is not currently available.
type ClientLookup func(accountID string) (*engine.Client, bool)

// # Config

// Config holds the construction parameters for a SubmissionManager.
type Config struct {
	// Accounts is the list of accounts the relay is configured to use.
	// Submit rejects requests for account IDs not in this list.
	Accounts []config.AccountConfig

	// Queue is the queue manager that persists and sequences outbound jobs.
	Queue *queue.QueueManager

	// Clients resolves a live engine.Client for a given account ID.
	// Used to resolve destination strings into Matrix room IDs at submission time.
	Clients ClientLookup

	// Store is the queue store used for idempotency pre-checks.
	Store persistence.QueueStore
}

// # SubmissionManager

// SubmissionManager is the single entry point for all outbound send requests.
// It validates requests, deduplicates by idempotency key, resolves destinations,
// and inserts jobs into the persistent outbound queue.
// It does not send Matrix events directly.
type SubmissionManager struct {
	accounts map[string]struct{}
	queue    *queue.QueueManager
	clients  ClientLookup
	store    persistence.QueueStore
}

// New constructs a SubmissionManager from cfg.
func New(cfg Config) *SubmissionManager {
	accounts := make(map[string]struct{}, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		accounts[a.ID] = struct{}{}
	}
	return &SubmissionManager{
		accounts: accounts,
		queue:    cfg.Queue,
		clients:  cfg.Clients,
		store:    cfg.Store,
	}
}

// UserID returns the Matrix user ID of the given account.
// Returns an empty string when the account is not currently available.
func (m *SubmissionManager) UserID(accountID string) string {
	client, ok := m.clients(accountID)
	if !ok {
		return ""
	}
	return string(client.UserID())
}

// SendDirect validates req, resolves the destination, and sends the message
// synchronously to the homeserver, returning the resulting Matrix event ID.
//
// Unlike Submit, SendDirect bypasses the queue and has no retry. Use this for
// ask-style interactions where the Matrix event ID is needed before a
// correlation can be registered.
func (m *SubmissionManager) SendDirect(ctx context.Context, req SubmitRequest) (string, error) {
	if err := validateRequest(req, m.accounts); err != nil {
		return "", err
	}
	client, ok := m.clients(req.AccountID)
	if !ok {
		return "", fmt.Errorf("submission: account %q is not currently available", req.AccountID)
	}
	roomID, err := client.ResolveRoom(ctx, req.Destination)
	if err != nil {
		return "", fmt.Errorf("submission: resolve destination %q: %w", req.Destination, err)
	}
	eventID, err := client.Send(ctx, roomID, req.Message)
	if err != nil {
		return "", fmt.Errorf("submission: send: %w", err)
	}
	return string(eventID), nil
}

// Submit validates req, deduplicates by idempotency key, resolves the
// destination to a Matrix room ID, and enqueues the job.
//
// Returns the job ID (new or existing) on success.
func (m *SubmissionManager) Submit(ctx context.Context, req SubmitRequest) (string, error) {
	// 1. Structural validation.
	if err := validateRequest(req, m.accounts); err != nil {
		return "", err
	}

	// 2. Idempotency pre-check: return early if this key was already submitted.
	if jobID, err := checkIdempotency(ctx, m.store, req.AccountID, req.IdempotencyKey); err != nil {
		return "", err
	} else if jobID != "" {
		return jobID, nil
	}

	// 3. Resolve destination to a Matrix room ID.
	client, ok := m.clients(req.AccountID)
	if !ok {
		return "", fmt.Errorf("submission: account %q is not currently available", req.AccountID)
	}
	roomID, err := client.ResolveRoom(ctx, req.Destination)
	if err != nil {
		return "", fmt.Errorf("submission: resolve destination %q: %w", req.Destination, err)
	}

	// 4. Enqueue (QueueManager also handles the idempotency key in case of races).
	job, err := m.queue.Enqueue(ctx, req.AccountID, roomID, req.Message, req.IdempotencyKey)
	if err != nil {
		return "", fmt.Errorf("submission: enqueue: %w", err)
	}
	return job.ID, nil
}
