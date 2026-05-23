package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// # Row types

// SyncStateRow represents the sync token state for one Matrix account.
type SyncStateRow struct {
	AccountID string
	NextBatch string
	UpdatedAt int64 // Unix ms
}

// QueueEntry represents one outbound message job in persistent storage.
type QueueEntry struct {
	ID             string
	AccountID      string
	RoomID         string
	Payload        string // JSON-encoded message payload
	State          string
	RetryCount     int
	ScheduledAt    int64  // Unix ms; 0 = immediately eligible
	IdempotencyKey string // empty = no idempotency key
	CreatedAt      int64
	UpdatedAt      int64
}

// CorrelationEntry represents one active ask or receive subscription.
type CorrelationEntry struct {
	ID              string
	Type            string // "ask" | "receive"
	AccountID       string
	RoomID          string
	OutboundEventID string
	FilterJSON      string
	TimeoutAt       int64
	State           string // "pending" | "resolved" | "expired"
	CreatedAt       int64
	UpdatedAt       int64
}

// APIKeyRow represents an API key record.
// KeyHash stores SHA-256(plaintext key); the plaintext is never persisted.
type APIKeyRow struct {
	ID              string
	KeyHash         string
	Name            string
	PermissionsJSON string
	CreatedAt       int64
	RevokedAt       *int64 // nil = active
}

// EventCacheEntry records a seen inbound event for deduplication.
type EventCacheEntry struct {
	EventID   string
	AccountID string
	SeenAt    int64
}

// # Interfaces

// SyncStore manages per-account sync token state.
type SyncStore interface {
	// GetNextBatch returns the stored sync token for accountID, or "" if none.
	GetNextBatch(ctx context.Context, accountID string) (string, error)
	// SetNextBatch upserts the sync token for accountID.
	SetNextBatch(ctx context.Context, accountID, token string) error
}

// QueueStore manages the persistent outbound message queue.
type QueueStore interface {
	// Enqueue inserts a new job.
	Enqueue(ctx context.Context, entry QueueEntry) error
	// GetByIdempotencyKey returns the existing job for the given key, or nil if none.
	GetByIdempotencyKey(ctx context.Context, accountID, key string) (*QueueEntry, error)
	// PullNext atomically claims the next eligible queued job for the account,
	// advances its state to "sending", and returns it. Returns nil, nil when none is available.
	PullNext(ctx context.Context, accountID string) (*QueueEntry, error)
	// UpdateState sets the state field of a job.
	UpdateState(ctx context.Context, id, state string) error
	// ScheduleRetry sets the job back to state "queued" with updated retry metadata.
	ScheduleRetry(ctx context.Context, id string, retryCount int, scheduledAt int64) error
	// MoveToDeadLetter marks a job as "dead_letter".
	MoveToDeadLetter(ctx context.Context, id string) error
	// RestoreStuck moves all "sending" jobs back to "queued" (crash recovery on startup).
	RestoreStuck(ctx context.Context) (int64, error)
	// ListByState returns jobs for an account whose state is one of states.
	ListByState(ctx context.Context, accountID string, states []string) ([]QueueEntry, error)
}

// CorrelationStore manages ask/receive correlation records.
type CorrelationStore interface {
	Insert(ctx context.Context, entry CorrelationEntry) error
	GetByID(ctx context.Context, id string) (*CorrelationEntry, error)
	UpdateState(ctx context.Context, id, state string) error
	// DeleteExpired removes pending entries whose timeout_at is before now (Unix ms).
	DeleteExpired(ctx context.Context, now int64) (int64, error)
	ListActive(ctx context.Context) ([]CorrelationEntry, error)
}

// APIKeyStore manages API key records.
type APIKeyStore interface {
	Insert(ctx context.Context, key APIKeyRow) error
	GetByHash(ctx context.Context, hash string) (*APIKeyRow, error)
	List(ctx context.Context) ([]APIKeyRow, error)
	// Delete removes a key record. Returns true if a row was deleted.
	Delete(ctx context.Context, id string) (bool, error)
	// SetRevoked records the revocation timestamp for a key.
	SetRevoked(ctx context.Context, id string, revokedAt int64) error
}

// EventCacheStore manages the inbound event deduplication cache.
type EventCacheStore interface {
	// Has reports whether (eventID, accountID) is already cached.
	Has(ctx context.Context, eventID, accountID string) (bool, error)
	Insert(ctx context.Context, entry EventCacheEntry) error
	// Prune deletes entries with seen_at < before (Unix ms). Returns number removed.
	Prune(ctx context.Context, before int64) (int64, error)
}

// # Constructors

// NewSyncStore returns a SyncStore backed by db.
func NewSyncStore(db *sql.DB) SyncStore { return &sqlSyncStore{db: db} }

// NewQueueStore returns a QueueStore backed by db.
func NewQueueStore(db *sql.DB) QueueStore { return &sqlQueueStore{db: db} }

// NewCorrelationStore returns a CorrelationStore backed by db.
func NewCorrelationStore(db *sql.DB) CorrelationStore { return &sqlCorrelationStore{db: db} }

// NewAPIKeyStore returns an APIKeyStore backed by db.
func NewAPIKeyStore(db *sql.DB) APIKeyStore { return &sqlAPIKeyStore{db: db} }

// NewEventCacheStore returns an EventCacheStore backed by db.
func NewEventCacheStore(db *sql.DB) EventCacheStore { return &sqlEventCacheStore{db: db} }

// # scanner helper

type scanner interface {
	Scan(dest ...any) error
}

// # SyncStore implementation

type sqlSyncStore struct{ db *sql.DB }

func (s *sqlSyncStore) GetNextBatch(ctx context.Context, accountID string) (string, error) {
	var token string
	err := s.db.QueryRowContext(ctx,
		`SELECT next_batch FROM sync_state WHERE account_id = ?`, accountID,
	).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("sync_store: get next_batch: %w", err)
	}
	return token, nil
}

func (s *sqlSyncStore) SetNextBatch(ctx context.Context, accountID, token string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (account_id, next_batch, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (account_id) DO UPDATE
		 SET next_batch = excluded.next_batch, updated_at = excluded.updated_at`,
		accountID, token, now,
	)
	if err != nil {
		return fmt.Errorf("sync_store: set next_batch: %w", err)
	}
	return nil
}

// # QueueStore implementation

type sqlQueueStore struct{ db *sql.DB }

const queueCols = `id, account_id, room_id, payload, state, retry_count, scheduled_at, idempotency_key, created_at, updated_at`

func scanQueueEntry(row scanner) (QueueEntry, error) {
	var e QueueEntry
	err := row.Scan(
		&e.ID, &e.AccountID, &e.RoomID, &e.Payload, &e.State,
		&e.RetryCount, &e.ScheduledAt, &e.IdempotencyKey, &e.CreatedAt, &e.UpdatedAt,
	)
	return e, err
}

func (s *sqlQueueStore) Enqueue(ctx context.Context, entry QueueEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO outbound_queue (`+queueCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.AccountID, entry.RoomID, entry.Payload, entry.State,
		entry.RetryCount, entry.ScheduledAt, entry.IdempotencyKey, entry.CreatedAt, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("queue_store: enqueue: %w", err)
	}
	return nil
}

func (s *sqlQueueStore) GetByIdempotencyKey(ctx context.Context, accountID, key string) (*QueueEntry, error) {
	if key == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+queueCols+` FROM outbound_queue WHERE account_id = ? AND idempotency_key = ?`,
		accountID, key,
	)
	e, err := scanQueueEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("queue_store: get by idempotency key: %w", err)
	}
	return &e, nil
}

func (s *sqlQueueStore) PullNext(ctx context.Context, accountID string) (*QueueEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("queue_store: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UnixMilli()
	row := tx.QueryRowContext(ctx,
		`SELECT `+queueCols+` FROM outbound_queue
		 WHERE account_id = ? AND state = 'queued' AND scheduled_at <= ?
		 ORDER BY scheduled_at ASC, created_at ASC
		 LIMIT 1`,
		accountID, now,
	)
	e, err := scanQueueEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("queue_store: pull next: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE outbound_queue SET state = 'sending', updated_at = ? WHERE id = ?`,
		now, e.ID,
	); err != nil {
		return nil, fmt.Errorf("queue_store: mark sending: %w", err)
	}
	e.State = "sending"
	e.UpdatedAt = now

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("queue_store: commit pull: %w", err)
	}
	return &e, nil
}

func (s *sqlQueueStore) UpdateState(ctx context.Context, id, state string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE outbound_queue SET state = ?, updated_at = ? WHERE id = ?`,
		state, time.Now().UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("queue_store: update state: %w", err)
	}
	return nil
}

func (s *sqlQueueStore) ScheduleRetry(ctx context.Context, id string, retryCount int, scheduledAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE outbound_queue SET state = 'queued', retry_count = ?, scheduled_at = ?, updated_at = ? WHERE id = ?`,
		retryCount, scheduledAt, time.Now().UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("queue_store: schedule retry: %w", err)
	}
	return nil
}

func (s *sqlQueueStore) MoveToDeadLetter(ctx context.Context, id string) error {
	return s.UpdateState(ctx, id, "dead_letter")
}

func (s *sqlQueueStore) RestoreStuck(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE outbound_queue SET state = 'queued', updated_at = ? WHERE state = 'sending'`,
		time.Now().UnixMilli(),
	)
	if err != nil {
		return 0, fmt.Errorf("queue_store: restore stuck: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqlQueueStore) ListByState(ctx context.Context, accountID string, states []string) ([]QueueEntry, error) {
	if len(states) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(states)), ",")
	args := make([]any, 0, len(states)+1)
	args = append(args, accountID)
	for _, st := range states {
		args = append(args, st)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+queueCols+` FROM outbound_queue WHERE account_id = ? AND state IN (`+placeholders+`)
		 ORDER BY scheduled_at ASC, created_at ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("queue_store: list by state: %w", err)
	}
	defer rows.Close()

	var entries []QueueEntry
	for rows.Next() {
		e, err := scanQueueEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("queue_store: scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// # CorrelationStore implementation

type sqlCorrelationStore struct{ db *sql.DB }

const corrCols = `id, type, account_id, room_id, outbound_event_id, filter_json, timeout_at, state, created_at, updated_at`

func scanCorrelationEntry(row scanner) (CorrelationEntry, error) {
	var e CorrelationEntry
	err := row.Scan(
		&e.ID, &e.Type, &e.AccountID, &e.RoomID, &e.OutboundEventID,
		&e.FilterJSON, &e.TimeoutAt, &e.State, &e.CreatedAt, &e.UpdatedAt,
	)
	return e, err
}

func (s *sqlCorrelationStore) Insert(ctx context.Context, entry CorrelationEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO correlation_state (`+corrCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Type, entry.AccountID, entry.RoomID, entry.OutboundEventID,
		entry.FilterJSON, entry.TimeoutAt, entry.State, entry.CreatedAt, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("correlation_store: insert: %w", err)
	}
	return nil
}

func (s *sqlCorrelationStore) GetByID(ctx context.Context, id string) (*CorrelationEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+corrCols+` FROM correlation_state WHERE id = ?`, id,
	)
	e, err := scanCorrelationEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("correlation_store: get by id: %w", err)
	}
	return &e, nil
}

func (s *sqlCorrelationStore) UpdateState(ctx context.Context, id, state string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE correlation_state SET state = ?, updated_at = ? WHERE id = ?`,
		state, time.Now().UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("correlation_store: update state: %w", err)
	}
	return nil
}

func (s *sqlCorrelationStore) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM correlation_state WHERE state = 'pending' AND timeout_at > 0 AND timeout_at < ?`, now,
	)
	if err != nil {
		return 0, fmt.Errorf("correlation_store: delete expired: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqlCorrelationStore) ListActive(ctx context.Context) ([]CorrelationEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+corrCols+` FROM correlation_state WHERE state = 'pending'`,
	)
	if err != nil {
		return nil, fmt.Errorf("correlation_store: list active: %w", err)
	}
	defer rows.Close()

	var entries []CorrelationEntry
	for rows.Next() {
		e, err := scanCorrelationEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("correlation_store: scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// # APIKeyStore implementation

type sqlAPIKeyStore struct{ db *sql.DB }

const apiKeyCols = `id, key_hash, name, permissions_json, created_at, revoked_at`

func scanAPIKey(row scanner) (APIKeyRow, error) {
	var k APIKeyRow
	err := row.Scan(&k.ID, &k.KeyHash, &k.Name, &k.PermissionsJSON, &k.CreatedAt, &k.RevokedAt)
	return k, err
}

func (s *sqlAPIKeyStore) Insert(ctx context.Context, key APIKeyRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (`+apiKeyCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		key.ID, key.KeyHash, key.Name, key.PermissionsJSON, key.CreatedAt, key.RevokedAt,
	)
	if err != nil {
		return fmt.Errorf("apikey_store: insert: %w", err)
	}
	return nil
}

func (s *sqlAPIKeyStore) GetByHash(ctx context.Context, hash string) (*APIKeyRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+apiKeyCols+` FROM api_keys WHERE key_hash = ?`, hash,
	)
	k, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("apikey_store: get by hash: %w", err)
	}
	return &k, nil
}

func (s *sqlAPIKeyStore) List(ctx context.Context) ([]APIKeyRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+apiKeyCols+` FROM api_keys ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("apikey_store: list: %w", err)
	}
	defer rows.Close()

	var keys []APIKeyRow
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("apikey_store: scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *sqlAPIKeyStore) Delete(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("apikey_store: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *sqlAPIKeyStore) SetRevoked(ctx context.Context, id string, revokedAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ?`, revokedAt, id,
	)
	if err != nil {
		return fmt.Errorf("apikey_store: set revoked: %w", err)
	}
	return nil
}

// # EventCacheStore implementation

type sqlEventCacheStore struct{ db *sql.DB }

func (s *sqlEventCacheStore) Has(ctx context.Context, eventID, accountID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM event_cache WHERE event_id = ? AND account_id = ?`,
		eventID, accountID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("event_cache_store: has: %w", err)
	}
	return count > 0, nil
}

func (s *sqlEventCacheStore) Insert(ctx context.Context, entry EventCacheEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO event_cache (event_id, account_id, seen_at) VALUES (?, ?, ?)
		 ON CONFLICT (event_id, account_id) DO NOTHING`,
		entry.EventID, entry.AccountID, entry.SeenAt,
	)
	if err != nil {
		return fmt.Errorf("event_cache_store: insert: %w", err)
	}
	return nil
}

func (s *sqlEventCacheStore) Prune(ctx context.Context, before int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM event_cache WHERE seen_at < ?`, before,
	)
	if err != nil {
		return 0, fmt.Errorf("event_cache_store: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
