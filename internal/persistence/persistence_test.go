package persistence_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ilamparithi-in/matfix/internal/persistence"
	_ "modernc.org/sqlite"
)

// openTestDB opens an in-memory SQLite database and runs migrations.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if err := persistence.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// # Migration tests

func TestMigrate_Idempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	// Run twice - must not error.
	for i := 0; i < 2; i++ {
		if err := persistence.Migrate(db); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}

	// All five migrations should be recorded.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 5 {
		t.Errorf("want 5 migration records, got %d", count)
	}
}

// # SyncStore tests

func TestSyncStore_GetSetNextBatch(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewSyncStore(db)
	ctx := context.Background()

	// Missing key returns empty string.
	got, err := store.GetNextBatch(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetNextBatch: %v", err)
	}
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}

	// Set then get.
	if err := store.SetNextBatch(ctx, "acc1", "s_abc"); err != nil {
		t.Fatalf("SetNextBatch: %v", err)
	}
	got, err = store.GetNextBatch(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetNextBatch after set: %v", err)
	}
	if got != "s_abc" {
		t.Errorf("want %q, got %q", "s_abc", got)
	}

	// Update (upsert) works.
	if err := store.SetNextBatch(ctx, "acc1", "s_xyz"); err != nil {
		t.Fatalf("SetNextBatch update: %v", err)
	}
	got, _ = store.GetNextBatch(ctx, "acc1")
	if got != "s_xyz" {
		t.Errorf("want %q after update, got %q", "s_xyz", got)
	}
}

// # QueueStore tests

func newEntry(id, accountID string) persistence.QueueEntry {
	now := time.Now().UnixMilli()
	return persistence.QueueEntry{
		ID:          id,
		AccountID:   accountID,
		RoomID:      "!room:example.com",
		Payload:     `{"type":"text","body":"hello"}`,
		State:       "queued",
		ScheduledAt: now - 1, // in the past → immediately eligible
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestQueueStore_EnqueueAndPullNext(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewQueueStore(db)
	ctx := context.Background()

	entry := newEntry("job-1", "acc1")
	if err := store.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := store.PullNext(ctx, "acc1")
	if err != nil {
		t.Fatalf("PullNext: %v", err)
	}
	if got == nil {
		t.Fatal("PullNext returned nil, want job")
	}
	if got.ID != "job-1" {
		t.Errorf("want job-1, got %s", got.ID)
	}
	if got.State != "sending" {
		t.Errorf("want state=sending, got %s", got.State)
	}

	// No more jobs available.
	got2, err := store.PullNext(ctx, "acc1")
	if err != nil {
		t.Fatalf("second PullNext: %v", err)
	}
	if got2 != nil {
		t.Errorf("want nil second PullNext, got %+v", got2)
	}
}

func TestQueueStore_IdempotencyKey(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewQueueStore(db)
	ctx := context.Background()

	entry := newEntry("job-2", "acc1")
	entry.IdempotencyKey = "idem-key-1"
	if err := store.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := store.GetByIdempotencyKey(ctx, "acc1", "idem-key-1")
	if err != nil {
		t.Fatalf("GetByIdempotencyKey: %v", err)
	}
	if got == nil || got.ID != "job-2" {
		t.Errorf("want job-2, got %v", got)
	}

	// Empty key always returns nil.
	got2, err := store.GetByIdempotencyKey(ctx, "acc1", "")
	if err != nil || got2 != nil {
		t.Errorf("want nil for empty key, got %v, err %v", got2, err)
	}
}

func TestQueueStore_RestoreStuck(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewQueueStore(db)
	ctx := context.Background()

	entry := newEntry("job-3", "acc1")
	if err := store.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Simulate a crash: job stuck in "sending".
	if err := store.UpdateState(ctx, "job-3", "sending"); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	n, err := store.RestoreStuck(ctx)
	if err != nil {
		t.Fatalf("RestoreStuck: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 restored, got %d", n)
	}

	// Job should now be pullable again.
	got, err := store.PullNext(ctx, "acc1")
	if err != nil || got == nil {
		t.Errorf("want job after restore, got %v, err %v", got, err)
	}
}

func TestQueueStore_DeadLetter(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewQueueStore(db)
	ctx := context.Background()

	entry := newEntry("job-4", "acc1")
	if err := store.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := store.MoveToDeadLetter(ctx, "job-4"); err != nil {
		t.Fatalf("MoveToDeadLetter: %v", err)
	}

	list, err := store.ListByState(ctx, "acc1", []string{"dead_letter"})
	if err != nil {
		t.Fatalf("ListByState: %v", err)
	}
	if len(list) != 1 || list[0].ID != "job-4" {
		t.Errorf("want dead-letter job-4, got %v", list)
	}
}

// # CorrelationStore tests

func TestCorrelationStore_InsertAndGet(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewCorrelationStore(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	entry := persistence.CorrelationEntry{
		ID:         "corr-1",
		Type:       "ask",
		AccountID:  "acc1",
		RoomID:     "!room:example.com",
		FilterJSON: `{}`,
		TimeoutAt:  now + 30000,
		State:      "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.Insert(ctx, entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.GetByID(ctx, "corr-1")
	if err != nil || got == nil {
		t.Fatalf("GetByID: got %v, err %v", got, err)
	}
	if got.Type != "ask" {
		t.Errorf("want type=ask, got %s", got.Type)
	}

	// Missing ID returns nil.
	got2, err := store.GetByID(ctx, "no-such-id")
	if err != nil || got2 != nil {
		t.Errorf("want nil for missing id, got %v, err %v", got2, err)
	}
}

func TestCorrelationStore_DeleteExpired(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewCorrelationStore(db)
	ctx := context.Background()

	past := time.Now().UnixMilli() - 10000
	entry := persistence.CorrelationEntry{
		ID:         "corr-2",
		Type:       "receive",
		AccountID:  "acc1",
		RoomID:     "!room:example.com",
		FilterJSON: `{}`,
		TimeoutAt:  past, // already timed out
		State:      "pending",
		CreatedAt:  past,
		UpdatedAt:  past,
	}
	if err := store.Insert(ctx, entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	n, err := store.DeleteExpired(ctx, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 deleted, got %d", n)
	}
}

// # APIKeyStore tests

func TestAPIKeyStore_InsertListDelete(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewAPIKeyStore(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	key := persistence.APIKeyRow{
		ID:              "key-1",
		KeyHash:         "sha256hash",
		Name:            "test key",
		PermissionsJSON: `{}`,
		CreatedAt:       now,
	}
	if err := store.Insert(ctx, key); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Lookup by hash.
	got, err := store.GetByHash(ctx, "sha256hash")
	if err != nil || got == nil {
		t.Fatalf("GetByHash: got %v, err %v", got, err)
	}
	if got.Name != "test key" {
		t.Errorf("want name='test key', got %s", got.Name)
	}

	// List.
	list, err := store.List(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("want 1 key in list, got %v, err %v", list, err)
	}

	// Delete.
	deleted, err := store.Delete(ctx, "key-1")
	if err != nil || !deleted {
		t.Errorf("want deleted=true, got %v, err %v", deleted, err)
	}
	list2, _ := store.List(ctx)
	if len(list2) != 0 {
		t.Errorf("want empty list after delete, got %v", list2)
	}
}

func TestAPIKeyStore_KeyHashNotRetrievableAfterRevoke(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewAPIKeyStore(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	key := persistence.APIKeyRow{
		ID:              "key-2",
		KeyHash:         "anotherhash",
		Name:            "revokable key",
		PermissionsJSON: `{}`,
		CreatedAt:       now,
	}
	if err := store.Insert(ctx, key); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	revokeAt := time.Now().UnixMilli()
	if err := store.SetRevoked(ctx, "key-2", revokeAt); err != nil {
		t.Fatalf("SetRevoked: %v", err)
	}

	got, err := store.GetByHash(ctx, "anotherhash")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got == nil {
		t.Fatal("want row after revoke (callers must check RevokedAt)")
	}
	if got.RevokedAt == nil || *got.RevokedAt != revokeAt {
		t.Errorf("want RevokedAt=%d, got %v", revokeAt, got.RevokedAt)
	}
}

// # EventCacheStore tests

func TestEventCacheStore_HasInsertPrune(t *testing.T) {
	db := openTestDB(t)
	store := persistence.NewEventCacheStore(db)
	ctx := context.Background()

	has, err := store.Has(ctx, "evt-1", "acc1")
	if err != nil || has {
		t.Errorf("want Has=false for unseen event, got %v, err %v", has, err)
	}

	now := time.Now().UnixMilli()
	if err := store.Insert(ctx, persistence.EventCacheEntry{EventID: "evt-1", AccountID: "acc1", SeenAt: now}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	has, err = store.Has(ctx, "evt-1", "acc1")
	if err != nil || !has {
		t.Errorf("want Has=true after insert, got %v, err %v", has, err)
	}

	// Insert is idempotent (ON CONFLICT DO NOTHING).
	if err := store.Insert(ctx, persistence.EventCacheEntry{EventID: "evt-1", AccountID: "acc1", SeenAt: now}); err != nil {
		t.Fatalf("duplicate Insert should not error: %v", err)
	}

	// Prune removes old entries.
	n, err := store.Prune(ctx, now+1)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 pruned, got %d", n)
	}
	has, _ = store.Has(ctx, "evt-1", "acc1")
	if has {
		t.Error("want Has=false after prune")
	}
}
