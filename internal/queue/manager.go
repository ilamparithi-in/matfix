package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"maunium.net/go/mautrix/id"

	"github.com/ilamparithi-in/matfix/internal/config"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # Config

// Config holds the construction parameters for a QueueManager.
type Config struct {
	Store  persistence.QueueStore
	Policy config.RetryPolicyConfig
}

// # QueueManager

// QueueManager is the sole owner of the outbound job state machine.
// All transitions on outbound_queue rows must go through this type.
type QueueManager struct {
	store  persistence.QueueStore
	policy config.RetryPolicyConfig
	// notify receives a signal (non-blocking) whenever a job is enqueued so
	// that idle workers can be woken without relying solely on polling.
	notify chan struct{}
}

// New constructs a QueueManager from cfg.
func New(cfg Config) *QueueManager {
	return &QueueManager{
		store:  cfg.Store,
		policy: cfg.Policy,
		notify: make(chan struct{}, 1),
	}
}

// Notify returns a channel that receives a value whenever a new job is
// enqueued. Workers select on this alongside a fallback poll ticker.
func (m *QueueManager) Notify() <-chan struct{} { return m.notify }

// Enqueue inserts a new job in state "queued" and returns it.
//
// If idempotencyKey is non-empty and a job with that key already exists for
// accountID, the existing job is returned without inserting a duplicate.
func (m *QueueManager) Enqueue(
	ctx context.Context,
	accountID string,
	roomID id.RoomID,
	req engine.SendRequest,
	idempotencyKey string,
) (*Job, error) {
	if idempotencyKey != "" {
		existing, err := m.store.GetByIdempotencyKey(ctx, accountID, idempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("queue: idempotency lookup: %w", err)
		}
		if existing != nil {
			return entryToJob(existing)
		}
	}

	payload, err := encodePayload(req)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	entry := persistence.QueueEntry{
		ID:             uuid.New().String(),
		AccountID:      accountID,
		RoomID:         string(roomID),
		Payload:        payload,
		State:          string(StateQueued),
		RetryCount:     0,
		ScheduledAt:    0,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := m.store.Enqueue(ctx, entry); err != nil {
		return nil, fmt.Errorf("queue: enqueue: %w", err)
	}

	// Wake any idle workers.
	select {
	case m.notify <- struct{}{}:
	default:
	}

	return entryToJob(&entry)
}

// PullNext atomically claims the next eligible job for accountID, advancing
// its state to "sending". Returns nil, nil when no eligible job is available.
func (m *QueueManager) PullNext(ctx context.Context, accountID string) (*Job, error) {
	entry, err := m.store.PullNext(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("queue: pull next: %w", err)
	}
	if entry == nil {
		return nil, nil
	}
	return entryToJob(entry)
}

// Transition sets the state of jobID to newState.
func (m *QueueManager) Transition(ctx context.Context, jobID string, newState State) error {
	if err := m.store.UpdateState(ctx, jobID, string(newState)); err != nil {
		return fmt.Errorf("queue: transition %s -> %s: %w", jobID, newState, err)
	}
	return nil
}

// ScheduleRetry schedules the next delivery attempt for jobID, or moves it to
// dead_letter if retries are exhausted.
//
// Returns (true, scheduledAt, nil) when a retry was scheduled.
// Returns (false, zero, nil) when the job was moved to dead_letter.
func (m *QueueManager) ScheduleRetry(
	ctx context.Context,
	jobID string,
	currentRetryCount int,
) (retried bool, scheduledAt time.Time, err error) {
	next := currentRetryCount + 1
	if IsExhausted(m.policy, next) {
		slog.Warn("queue: retries exhausted, moving to dead_letter",
			"job_id", jobID,
			"retry_count", next,
		)
		if err := m.MoveToDeadLetter(ctx, jobID); err != nil {
			return false, time.Time{}, err
		}
		return false, time.Time{}, nil
	}

	scheduledAt = NextScheduledAt(m.policy, next)
	if err := m.store.ScheduleRetry(ctx, jobID, next, scheduledAt.UnixMilli()); err != nil {
		return false, time.Time{}, fmt.Errorf("queue: schedule retry: %w", err)
	}
	return true, scheduledAt, nil
}

// MoveToDeadLetter permanently marks jobID as dead_letter.
func (m *QueueManager) MoveToDeadLetter(ctx context.Context, jobID string) error {
	if err := m.store.MoveToDeadLetter(ctx, jobID); err != nil {
		return fmt.Errorf("queue: move to dead_letter: %w", err)
	}
	return nil
}

// # Internal helpers

// entryToJob converts a persistence.QueueEntry to a Job.
func entryToJob(e *persistence.QueueEntry) (*Job, error) {
	req, err := decodePayload(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("queue: decode job %s: %w", e.ID, err)
	}
	return &Job{
		ID:             e.ID,
		AccountID:      e.AccountID,
		RoomID:         id.RoomID(e.RoomID),
		Request:        req,
		State:          State(e.State),
		RetryCount:     e.RetryCount,
		ScheduledAt:    time.UnixMilli(e.ScheduledAt),
		IdempotencyKey: e.IdempotencyKey,
		CreatedAt:      time.UnixMilli(e.CreatedAt),
		UpdatedAt:      time.UnixMilli(e.UpdatedAt),
	}, nil
}
