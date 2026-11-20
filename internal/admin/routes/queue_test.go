package routes_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"github.com/ilamparithi-in/matfix/internal/admin/routes"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/queue"
)

func setupQueueTestStore(t *testing.T) persistence.QueueStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if err := persistence.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return persistence.NewQueueStore(db)
}

func setupQueueRouter(store persistence.QueueStore) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/queue/dead-letter", routes.ListDeadLetterHandler(store))
	r.Post("/queue/redrive/{id}", routes.RedriveQueueHandler(store))
	r.Post("/queue/redrive-all", routes.RedriveAllQueueHandler(store))
	return r
}

func TestAdminRoutes_QueueDeadLetterAndRedrive(t *testing.T) {
	store := setupQueueTestStore(t)
	r := setupQueueRouter(store)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	entry1 := persistence.QueueEntry{
		ID:          "job-1",
		AccountID:   "bot1",
		RoomID:      "!room1:example.org",
		Payload:     `{"type":"text_message","content":{"body":"hi"}}`,
		State:       "dead_letter",
		RetryCount:  5,
		ScheduledAt: now,
		LastError:   "M_FORBIDDEN: not in room",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	entry2 := persistence.QueueEntry{
		ID:          "job-2",
		AccountID:   "bot2",
		RoomID:      "!room2:example.org",
		Payload:     `{"type":"text_message","content":{"body":"hello"}}`,
		State:       "dead_letter",
		RetryCount:  5,
		ScheduledAt: now,
		LastError:   "federation timeout",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Enqueue(ctx, entry1); err != nil {
		t.Fatalf("enqueue entry1: %v", err)
	}
	if err := store.Enqueue(ctx, entry2); err != nil {
		t.Fatalf("enqueue entry2: %v", err)
	}

	// 1. List dead-letter jobs
	req := httptest.NewRequest(http.MethodGet, "/queue/dead-letter", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var dlJobs []routes.DeadLetterEntry
	if err := json.NewDecoder(w.Body).Decode(&dlJobs); err != nil {
		t.Fatalf("decode dead letter response: %v", err)
	}
	if len(dlJobs) != 2 {
		t.Fatalf("expected 2 dead letter jobs, got %d", len(dlJobs))
	}

	// Filter by account_id
	reqAcc := httptest.NewRequest(http.MethodGet, "/queue/dead-letter?account_id=bot1", nil)
	wAcc := httptest.NewRecorder()
	r.ServeHTTP(wAcc, reqAcc)
	if wAcc.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wAcc.Code)
	}
	var dlJobsAcc []routes.DeadLetterEntry
	if err := json.NewDecoder(wAcc.Body).Decode(&dlJobsAcc); err != nil {
		t.Fatalf("decode dead letter response: %v", err)
	}
	if len(dlJobsAcc) != 1 || dlJobsAcc[0].ID != "job-1" {
		t.Fatalf("expected 1 dead letter job for bot1, got %v", dlJobsAcc)
	}
	if dlJobsAcc[0].LastError != "M_FORBIDDEN: not in room" {
		t.Errorf("expected LastError 'M_FORBIDDEN: not in room', got %q", dlJobsAcc[0].LastError)
	}

	// 2. Redrive job-1
	reqRedrive := httptest.NewRequest(http.MethodPost, "/queue/redrive/job-1", nil)
	wRedrive := httptest.NewRecorder()
	r.ServeHTTP(wRedrive, reqRedrive)
	if wRedrive.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wRedrive.Code)
	}

	// Verify job-1 is no longer in dead-letter
	reqCheck := httptest.NewRequest(http.MethodGet, "/queue/dead-letter", nil)
	wCheck := httptest.NewRecorder()
	r.ServeHTTP(wCheck, reqCheck)
	var dlJobsRemaining []routes.DeadLetterEntry
	_ = json.NewDecoder(wCheck.Body).Decode(&dlJobsRemaining)
	if len(dlJobsRemaining) != 1 || dlJobsRemaining[0].ID != "job-2" {
		t.Fatalf("expected only job-2 remaining in dead-letter, got %v", dlJobsRemaining)
	}

	// 3. Redrive 404 for non-existent or already queued job
	req404 := httptest.NewRequest(http.MethodPost, "/queue/redrive/non-existent", nil)
	w404 := httptest.NewRecorder()
	r.ServeHTTP(w404, req404)
	if w404.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w404.Code)
	}

	// 4. Redrive all
	reqRedriveAll := httptest.NewRequest(http.MethodPost, "/queue/redrive-all", nil)
	wRedriveAll := httptest.NewRecorder()
	r.ServeHTTP(wRedriveAll, reqRedriveAll)
	if wRedriveAll.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wRedriveAll.Code)
	}
	var redriveAllRes struct {
		Count int64 `json:"count"`
	}
	_ = json.NewDecoder(wRedriveAll.Body).Decode(&redriveAllRes)
	if redriveAllRes.Count != 1 {
		t.Errorf("expected 1 job redriven, got %d", redriveAllRes.Count)
	}

	// Dead letter queue should now be completely empty
	reqEmpty := httptest.NewRequest(http.MethodGet, "/queue/dead-letter", nil)
	wEmpty := httptest.NewRecorder()
	r.ServeHTTP(wEmpty, reqEmpty)
	var emptyJobs []routes.DeadLetterEntry
	_ = json.NewDecoder(wEmpty.Body).Decode(&emptyJobs)
	if len(emptyJobs) != 0 {
		t.Errorf("expected 0 jobs in dead-letter, got %d", len(emptyJobs))
	}
}

func TestAdminRoutes_QueueRedriverWithQueueManager(t *testing.T) {
	store := setupQueueTestStore(t)
	mgr := queue.New(queue.Config{
		Store: store,
	})
	r := chi.NewRouter()
	r.Post("/queue/redrive/{id}", routes.RedriveQueueHandler(mgr))
	r.Post("/queue/redrive-all", routes.RedriveAllQueueHandler(mgr))

	now := time.Now().UnixMilli()
	entry := persistence.QueueEntry{
		ID:          "job-wake",
		AccountID:   "bot1",
		RoomID:      "!room1:example.org",
		Payload:     `{"type":"text_message","content":{"body":"hi"}}`,
		State:       "dead_letter",
		RetryCount:  5,
		ScheduledAt: now,
		LastError:   "network err",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Enqueue(context.Background(), entry); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Drain any prior notifications
	select {
	case <-mgr.Notify():
	default:
	}

	req := httptest.NewRequest(http.MethodPost, "/queue/redrive/job-wake", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify notification was sent to wake workers
	select {
	case <-mgr.Notify():
		// Success: worker notification fired
	case <-time.After(500 * time.Millisecond):
		t.Error("expected notification on mgr.Notify() after redrive")
	}
}
