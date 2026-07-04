package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apires "github.com/ilamparithi-in/matfix/internal/api/response"
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

func TestJobStatusHandler(t *testing.T) {
	db := openTestDB(t)
	queueStore := persistence.NewQueueStore(db)
	corrStore := persistence.NewCorrelationStore(db)
	apiKeyStore := persistence.NewAPIKeyStore(db)

	ctx := context.Background()

	// 1. Generate an API Key and insert it into key store.
	rawKey := "test-api-key"
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	perms := Permissions{
		Accounts: []string{"bot"},
		Routes:   []string{"jobs", "send"},
	}
	permsJSON, _ := json.Marshal(perms)

	err := apiKeyStore.Insert(ctx, persistence.APIKeyRow{
		ID:              "key-1",
		KeyHash:         keyHash,
		Name:            "Test Key",
		PermissionsJSON: string(permsJSON),
		CreatedAt:       time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("failed to insert API key: %v", err)
	}

	// 2. Insert a queued job into outbound_queue.
	err = queueStore.Enqueue(ctx, persistence.QueueEntry{
		ID:             "job-send-1",
		AccountID:      "bot",
		RoomID:         "!room1:example.org",
		Payload:        `{"type":"text","body":"hello"}`,
		State:          "acknowledged",
		RetryCount:     0,
		ScheduledAt:    0,
		IdempotencyKey: "idem-1",
		CreatedAt:      1000,
		UpdatedAt:      2000,
		MatrixEventID:  "$event1:example.org",
	})
	if err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	// 3. Insert a correlation entry into correlation_state.
	err = corrStore.Insert(ctx, persistence.CorrelationEntry{
		ID:               "corr-ask-1",
		Type:             "ask",
		AccountID:        "bot",
		RoomID:           "!room1:example.org",
		OutboundEventID:  "$event1:example.org",
		FilterJSON:       "{}",
		TimeoutAt:        20000,
		State:            "resolved",
		CreatedAt:        1000,
		UpdatedAt:        2000,
		ResolvedEventIDs: `["$event-reply:example.org"]`,
	})
	if err != nil {
		t.Fatalf("failed to insert correlation: %v", err)
	}

	// Set up the router using our test dependencies.
	cfg := Config{
		APIKeyStore: apiKeyStore,
		QueueStore:  queueStore,
		CorrStore:   corrStore,
	}
	rl := newRateLimiter()
	router := buildRouter(cfg, rl)

	// Sub-test A: Get status of a queued send job.
	t.Run("GET send job success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/jobs/job-send-1", nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("got status %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body.String())
		}

		var res apires.JobStatusResponse
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if res.JobID != "job-send-1" || res.Type != "send" || res.State != "acknowledged" || res.MatrixEventID != "$event1:example.org" {
			t.Errorf("unexpected job details: %+v", res)
		}
	})

	// Sub-test B: Get status of a correlation ask job.
	t.Run("GET ask job success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/jobs/corr-ask-1", nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
		}

		var res apires.JobStatusResponse
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if res.JobID != "corr-ask-1" || res.Type != "ask" || res.State != "resolved" || len(res.ResolvedEventIDs) != 1 || res.ResolvedEventIDs[0] != "$event-reply:example.org" {
			t.Errorf("unexpected job details: %+v", res)
		}
	})

	// Sub-test C: Job not found (404).
	t.Run("GET job not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/jobs/non-existent", nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("got status %d, want 404", w.Code)
		}
	})

	// Sub-test D: Authorization check (403).
	t.Run("GET forbidden account", func(t *testing.T) {
		// Insert a job belonging to a different account "other-bot".
		err = queueStore.Enqueue(ctx, persistence.QueueEntry{
			ID:            "job-forbidden",
			AccountID:     "other-bot",
			RoomID:        "!room1:example.org",
			Payload:       `{"type":"text"}`,
			State:         "queued",
			CreatedAt:     1000,
			UpdatedAt:     2000,
			MatrixEventID: "",
		})
		if err != nil {
			t.Fatalf("failed to enqueue job: %v", err)
		}

		req := httptest.NewRequest("GET", "/v1/jobs/job-forbidden", nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("got status %d, want 403", w.Code)
		}
	})
}
