package routes_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"github.com/ilamparithi-in/matfix/internal/admin/routes"
	"github.com/ilamparithi-in/matfix/internal/correlation"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/webhook"
)

func setupTestManager(t *testing.T) (*webhook.WebhookManager, persistence.WebhookStore) {
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
	store := persistence.NewWebhookStore(db)

	tmpDir := t.TempDir()
	rulesFile := filepath.Join(tmpDir, "webhooks.yaml")

	mgr, err := webhook.NewManager(webhook.Config{
		FilePath: rulesFile,
		Store:    store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	return mgr, store
}

func setupRouter(mgr *webhook.WebhookManager) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/webhooks", routes.ListWebhooksHandler(mgr))
	r.Post("/webhooks", routes.CreateWebhookHandler(mgr))
	r.Get("/webhooks/{id}", routes.GetWebhookHandler(mgr))
	r.Delete("/webhooks/{id}", routes.DeleteWebhookHandler(mgr))
	r.Post("/webhooks/reload", routes.ReloadWebhooksHandler(mgr))
	r.Get("/webhooks/queue", routes.WebhookQueueHandler(mgr))
	r.Post("/webhooks/queue/redrive/{id}", routes.RedriveWebhookHandler(mgr))
	r.Post("/webhooks/queue/redrive-all", routes.RedriveAllWebhooksHandler(mgr))
	return r
}

func TestAdminRoutes_WebhooksCRUD(t *testing.T) {
	mgr, _ := setupTestManager(t)
	r := setupRouter(mgr)

	// 1. Initially empty
	req := httptest.NewRequest(http.MethodGet, "/webhooks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /webhooks: status %d", w.Code)
	}
	var list []webhook.WebhookRule
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 rules, got %d", len(list))
	}

	// 2. Create rule
	newRule := webhook.WebhookRule{
		ID:  "hook-1",
		URL: "https://example.com/hook",
		Filter: correlation.FilterNode{
			RoomID: &correlation.StringSetFilter{Include: []string{"!room:example.com"}},
		},
	}
	body, _ := json.Marshal(newRule)
	req = httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /webhooks: status %d, body: %s", w.Code, w.Body.String())
	}

	// 3. Get rule
	req = httptest.NewRequest(http.MethodGet, "/webhooks/hook-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /webhooks/hook-1: status %d", w.Code)
	}
	var fetched webhook.WebhookRule
	_ = json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.ID != "hook-1" || fetched.URL != "https://example.com/hook" {
		t.Errorf("unexpected fetched rule: %+v", fetched)
	}

	// 4. List now has 1
	req = httptest.NewRequest(http.MethodGet, "/webhooks", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	list = nil
	_ = json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 {
		t.Errorf("expected 1 rule, got %d", len(list))
	}

	// 5. Delete rule
	req = httptest.NewRequest(http.MethodDelete, "/webhooks/hook-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /webhooks/hook-1: status %d", w.Code)
	}

	// 6. Get deleted rule returns 404
	req = httptest.NewRequest(http.MethodGet, "/webhooks/hook-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for deleted rule, got %d", w.Code)
	}
}

func TestAdminRoutes_QueueAndRedrive(t *testing.T) {
	mgr, store := setupTestManager(t)
	r := setupRouter(mgr)

	// Record a dead-letter entry in DB
	err := store.RecordFailure(context.Background(), persistence.WebhookQueueEntry{
		ID:             "entry-dead-1",
		WebhookID:      "hook-1",
		URL:            "https://example.com/dl",
		Payload:        `{}`,
		State:          "dead_letter",
		RetryCount:     5,
		MaxRetries:     5,
		ScheduledAt:    1000,
		LastStatusCode: 500,
		LastError:      "fatal error",
		CreatedAt:      1000,
		UpdatedAt:      1000,
	})
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	// Check /webhooks/queue
	req := httptest.NewRequest(http.MethodGet, "/webhooks/queue", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /webhooks/queue: status %d", w.Code)
	}
	var qRes routes.WebhookQueueResponse
	_ = json.NewDecoder(w.Body).Decode(&qRes)
	if qRes.DeadLetter != 1 || qRes.Failed != 0 || qRes.Sending != 0 {
		t.Errorf("expected dead_letter=1 failed=0 sending=0, got %+v", qRes)
	}

	// Redrive single
	req = httptest.NewRequest(http.MethodPost, "/webhooks/queue/redrive/entry-dead-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /webhooks/queue/redrive/entry-dead-1: status %d", w.Code)
	}

	// Check queue again: should be failed=1, dead_letter=0
	req = httptest.NewRequest(http.MethodGet, "/webhooks/queue", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	qRes = routes.WebhookQueueResponse{}
	_ = json.NewDecoder(w.Body).Decode(&qRes)
	if qRes.DeadLetter != 0 || qRes.Failed != 1 || qRes.Sending != 0 {
		t.Errorf("expected dead_letter=0 failed=1 sending=0 after redrive, got %+v", qRes)
	}
}
