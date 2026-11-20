package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/correlation"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/webhook"
)

// openTestDB creates an in-memory SQLite database and applies all migrations.
func openTestDB(t *testing.T) (*sql.DB, persistence.WebhookStore) {
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
	return db, persistence.NewWebhookStore(db)
}

func countQueueEntries(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_queue`).Scan(&count); err != nil {
		t.Fatalf("count webhook_queue: %v", err)
	}
	return count
}

func TestWebhook_ZeroDBHappyPath(t *testing.T) {
	db, store := openTestDB(t)

	var (
		receivedCh = make(chan apires.EventPayload, 1)
		headerCh   = make(chan http.Header, 1)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headerCh <- r.Header.Clone()
		var p apires.EventPayload
		_ = json.NewDecoder(r.Body).Decode(&p)
		receivedCh <- p
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	rulesFile := filepath.Join(tmpDir, "webhooks.yaml")

	eventBus := bus.NewLocalBus()
	mgr, err := webhook.NewManager(webhook.Config{
		FilePath:       rulesFile,
		Store:          store,
		Bus:            eventBus,
		DefaultTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Stop()

	// Add webhook rule targeting test server
	rule := webhook.WebhookRule{
		ID:     "happy-path-hook",
		URL:    server.URL,
		Secret: "secret123",
		Headers: map[string]string{
			"X-Custom-Env": "testing",
		},
		Filter: correlation.FilterNode{
			RoomID:    &correlation.StringSetFilter{Include: []string{"!target:matrix.org"}},
			EventType: &correlation.StringSetFilter{Include: []string{"m.room.message"}},
			BodyRegex: "hello webhook",
		},
	}
	if err := mgr.AddOrUpdate(rule); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	// Publish matching event
	eventBus.Publish(bus.EventEnvelope{
		AccountID: "bot1",
		Type:      bus.EventInboundMessage,
		RoomID:    "!target:matrix.org",
		Payload: bus.InboundMessageEvent{
			EventID:   "$event_happy",
			SenderID:  "@alice:matrix.org",
			Body:      "hello webhook world",
			MsgType:   "m.text",
			Timestamp: time.Now(),
		},
	})

	select {
	case payload := <-receivedCh:
		if payload.EventID != "$event_happy" {
			t.Errorf("want event ID $event_happy, got %s", payload.EventID)
		}
		if payload.RoomID != "!target:matrix.org" {
			t.Errorf("want room !target:matrix.org, got %s", payload.RoomID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for webhook HTTP delivery")
	}

	headers := <-headerCh
	if headers.Get("X-Custom-Env") != "testing" {
		t.Errorf("want X-Custom-Env: testing, got %s", headers.Get("X-Custom-Env"))
	}
	if headers.Get("X-Matfix-Signature-256") == "" {
		t.Error("expected X-Matfix-Signature-256 header")
	}

	// ZERO-DB VERIFICATION:
	// Happy path must not have written ANY records to webhook_queue!
	if c := countQueueEntries(t, db); c != 0 {
		t.Errorf("Zero-DB Happy Path violation: expected 0 SQLite records, found %d", c)
	}
}

func TestWebhook_HMACSignature(t *testing.T) {
	secret := "test-hmac-key"
	var receivedSig string
	var rawBody []byte
	var wg sync.WaitGroup
	wg.Add(1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		receivedSig = r.Header.Get("X-Matfix-Signature-256")
		rawBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := webhook.NewDispatcher()
	res := dispatcher.Dispatch(context.Background(), webhook.DispatchParams{
		DeliveryID: uuid.New().String(),
		WebhookID:  "hmac-hook",
		URL:        server.URL,
		Secret:     secret,
		EventType:  "m.room.message",
		Payload:    []byte(`{"test":"payload"}`),
		Timeout:    5 * time.Second,
	})
	if !res.Success {
		t.Fatalf("dispatch failed: %v", res.Err)
	}
	wg.Wait()

	// Compute expected HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if receivedSig != expectedSig {
		t.Errorf("HMAC mismatch: got %s, want %s", receivedSig, expectedSig)
	}
}

func TestWebhook_FailurePersistenceAndRetry(t *testing.T) {
	db, store := openTestDB(t)

	var shouldFail atomic.Bool
	shouldFail.Store(true)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if shouldFail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	rulesFile := filepath.Join(tmpDir, "webhooks.yaml")

	eventBus := bus.NewLocalBus()
	mgr, err := webhook.NewManager(webhook.Config{
		FilePath:       rulesFile,
		Store:          store,
		Bus:            eventBus,
		DefaultTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Stop()

	rule := webhook.WebhookRule{
		ID:     "retry-hook",
		URL:    server.URL,
		Secret: "sec",
		Filter: correlation.FilterNode{
			RoomID: &correlation.StringSetFilter{Include: []string{"!retry:matrix.org"}},
		},
		RetryPolicy: webhook.RetryPolicyConfig{
			MaxRetries:      3,
			InitialInterval: "5s",
			MaxInterval:     "30s",
		},
	}
	if err := mgr.AddOrUpdate(rule); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	// Publish event when server is failing
	eventBus.Publish(bus.EventEnvelope{
		AccountID: "bot1",
		Type:      bus.EventInboundMessage,
		RoomID:    "!retry:matrix.org",
		Payload: bus.InboundMessageEvent{
			EventID:   "$fail_event",
			SenderID:  "@bob:matrix.org",
			Body:      "fail please",
			MsgType:   "m.text",
			Timestamp: time.Now(),
		},
	})

	// Wait for failure entry to be committed to SQLite
	var entry *persistence.WebhookQueueEntry
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := store.ListByState(context.Background(), "failed", 10)
		if err == nil && len(entries) > 0 {
			entry = &entries[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if entry == nil {
		t.Fatal("expected failed webhook entry in SQLite database, none found")
	}

	if entry.LastStatusCode != http.StatusInternalServerError {
		t.Errorf("want last status code 500, got %d", entry.LastStatusCode)
	}
	if entry.RetryCount != 1 {
		t.Errorf("want initial retry_count=1 after fast path failure, got %d", entry.RetryCount)
	}

	// Now fix server: return 200 OK
	shouldFail.Store(false)

	// Update scheduled_at in DB to now so retry worker picks it up immediately
	_, err = db.Exec(`UPDATE webhook_queue SET scheduled_at = ? WHERE id = ?`, time.Now().UnixMilli()-1000, entry.ID)
	if err != nil {
		t.Fatalf("update scheduled_at: %v", err)
	}

	// Wait for retry worker to deliver and delete entry
	deletedDeadline := time.Now().Add(5 * time.Second)
	var deleted bool
	for time.Now().Before(deletedDeadline) {
		e, err := store.GetByID(context.Background(), entry.ID)
		if err == nil && e == nil {
			deleted = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !deleted {
		t.Fatal("expected entry to be deleted after successful retry delivery")
	}
}

func TestWebhook_ClientErrorDeadLetter(t *testing.T) {
	_, store := openTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404 client error
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	rulesFile := filepath.Join(tmpDir, "webhooks.yaml")

	eventBus := bus.NewLocalBus()
	mgr, err := webhook.NewManager(webhook.Config{
		FilePath:       rulesFile,
		Store:          store,
		Bus:            eventBus,
		DefaultTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Stop()

	rule := webhook.WebhookRule{
		ID:  "dl-hook",
		URL: server.URL,
		Filter: correlation.FilterNode{
			RoomID: &correlation.StringSetFilter{Include: []string{"!dl:matrix.org"}},
		},
	}
	if err := mgr.AddOrUpdate(rule); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	eventBus.Publish(bus.EventEnvelope{
		AccountID: "bot1",
		Type:      bus.EventInboundMessage,
		RoomID:    "!dl:matrix.org",
		Payload: bus.InboundMessageEvent{
			EventID:   "$dl_event",
			SenderID:  "@bob:matrix.org",
			Body:      "test dead letter",
			MsgType:   "m.text",
			Timestamp: time.Now(),
		},
	})

	// 4xx errors should be dead-lettered immediately
	deadline := time.Now().Add(3 * time.Second)
	var deadLetterFound bool
	var dlEntry persistence.WebhookQueueEntry
	for time.Now().Before(deadline) {
		entries, err := store.ListByState(context.Background(), "dead_letter", 10)
		if err == nil && len(entries) > 0 {
			deadLetterFound = true
			dlEntry = entries[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !deadLetterFound {
		t.Fatal("expected 404 response to immediately dead-letter, but none found in dead_letter state")
	}

	// Test redrive
	if err := mgr.Redrive(context.Background(), dlEntry.ID); err != nil {
		t.Fatalf("Redrive: %v", err)
	}

	// Verify state moved to 'failed' with retry_count=0
	updated, err := store.GetByID(context.Background(), dlEntry.ID)
	if err != nil || updated == nil {
		t.Fatalf("GetByID after redrive: %v", err)
	}
	if updated.State != "failed" || updated.RetryCount != 0 {
		t.Errorf("after redrive want state='failed' retry_count=0, got state=%q retry_count=%d", updated.State, updated.RetryCount)
	}
}

func TestWebhook_RoomFilterIsolation(t *testing.T) {
	_, store := openTestDB(t)

	var hook1Called, hook2Called atomic.Bool

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hook1Called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hook2Called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	tmpDir := t.TempDir()
	rulesFile := filepath.Join(tmpDir, "webhooks.yaml")

	eventBus := bus.NewLocalBus()
	mgr, err := webhook.NewManager(webhook.Config{
		FilePath: rulesFile,
		Store:    store,
		Bus:      eventBus,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Stop()

	mgr.AddOrUpdate(webhook.WebhookRule{
		ID:     "room-1-hook",
		URL:    server1.URL,
		Filter: correlation.FilterNode{RoomID: &correlation.StringSetFilter{Include: []string{"!room1:matrix.org"}}},
	})
	mgr.AddOrUpdate(webhook.WebhookRule{
		ID:     "room-2-hook",
		URL:    server2.URL,
		Filter: correlation.FilterNode{RoomID: &correlation.StringSetFilter{Include: []string{"!room2:matrix.org"}}},
	})

	// Publish to room 1 only
	eventBus.Publish(bus.EventEnvelope{
		AccountID: "bot1",
		Type:      bus.EventInboundMessage,
		RoomID:    "!room1:matrix.org",
		Payload: bus.InboundMessageEvent{
			EventID:   "$r1_event",
			SenderID:  "@bob:matrix.org",
			Body:      "room1 only",
			MsgType:   "m.text",
			Timestamp: time.Now(),
		},
	})

	time.Sleep(150 * time.Millisecond)

	if !hook1Called.Load() {
		t.Error("expected hook 1 to be called for !room1:matrix.org")
	}
	if hook2Called.Load() {
		t.Error("hook 2 should NOT be called for !room1:matrix.org")
	}
}

func TestWebhook_FilePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	rulesFile := filepath.Join(tmpDir, "webhooks.yaml")

	mgr, err := webhook.NewManager(webhook.Config{
		FilePath: rulesFile,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	rule := webhook.WebhookRule{
		ID:  "persist-test",
		URL: "http://example.com/webhook",
		Filter: correlation.FilterNode{
			RoomID: &correlation.StringSetFilter{Include: []string{"!test:matrix.org"}},
		},
	}

	if err := mgr.AddOrUpdate(rule); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	// Verify file was written to disk
	data, err := os.ReadFile(rulesFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("rules file is empty")
	}

	// Create new manager from same file, verify rule was persisted
	mgr2, err := webhook.NewManager(webhook.Config{
		FilePath: rulesFile,
	})
	if err != nil {
		t.Fatalf("NewManager 2: %v", err)
	}
	got := mgr2.Get("persist-test")
	if got == nil || got.URL != "http://example.com/webhook" {
		t.Fatalf("expected persisted rule to be loaded, got %+v", got)
	}

	// Delete
	deleted, err := mgr.Delete("persist-test")
	if err != nil || !deleted {
		t.Fatalf("Delete: deleted=%t, err=%v", deleted, err)
	}
	if mgr.Get("persist-test") != nil {
		t.Error("rule should be deleted from memory")
	}
}

func TestFileManager_Save_ReadOnlyDirFallback(t *testing.T) {
	dir := t.TempDir()
	rulesFile := filepath.Join(dir, "webhooks.yaml")
	if err := os.WriteFile(rulesFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Make directory read-only so creating temp files inside it fails,
	// simulating Docker containers where rootfs is read-only and webhooks.yaml is a bind-mount.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	fm := webhook.NewFileManager(rulesFile)
	rules := []webhook.WebhookRule{
		{
			ID:  "fallback-test",
			URL: "https://example.com/hook",
		},
	}

	if err := fm.Save(rules); err != nil {
		t.Fatalf("Save with read-only dir fallback failed: %v", err)
	}

	loaded, err := fm.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "fallback-test" {
		t.Fatalf("loaded rules mismatch: got %+v", loaded)
	}
}
