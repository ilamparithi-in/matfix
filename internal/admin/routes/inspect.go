package routes

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ilamparithi-in/matfix/internal/account"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/queue"
)

// # Response types

type DeadLetterEntry struct {
	ID             string `json:"id"`
	AccountID      string `json:"account_id"`
	RoomID         string `json:"room_id"`
	RetryCount     int    `json:"retry_count"`
	LastError      string `json:"last_error"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type accountStatusEntry struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type accountsResponse struct {
	Accounts []accountStatusEntry `json:"accounts"`
}

type queueResponse struct {
	Queued     int `json:"queued"`
	Sending    int `json:"sending"`
	Failed     int `json:"failed"`
	DeadLetter int `json:"dead_letter"`
}

type subscriptionsResponse struct {
	Asks     int `json:"asks"`
	Receives int `json:"receives"`
}

// # Handlers

// AccountsHandler handles GET /accounts.
//
// It returns the availability status of every configured Matrix account.
func AccountsHandler(accounts *account.AccountManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all := accounts.All()
		entries := make([]accountStatusEntry, 0, len(all))
		for _, actx := range all {
			e := accountStatusEntry{
				ID:        actx.AccountID(),
				Available: actx.IsAvailable(),
			}
			if err := actx.Err(); err != nil {
				e.Error = err.Error()
			}
			entries = append(entries, e)
		}
		writeJSON(w, http.StatusOK, accountsResponse{Accounts: entries})
	}
}

// QueueHandler handles GET /queue.
//
// It returns the count of outbound jobs in each significant state across all
// configured accounts.
func QueueHandler(accounts *account.AccountManager, store persistence.QueueStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all := accounts.All()
		var resp queueResponse
		for _, actx := range all {
			entries, err := store.ListByState(r.Context(), actx.AccountID(), []string{
				string(queue.StateQueued), string(queue.StateSending),
				string(queue.StateFailed), string(queue.StateDeadLetter),
			})
			if err != nil {
				slog.Error("admin: queue inspect failed", "account_id", actx.AccountID(), "error", err)
				writeError(w, http.StatusInternalServerError, "failed to query queue", "internal_error")
				return
			}
			for _, e := range entries {
				switch e.State {
				case string(queue.StateQueued):
					resp.Queued++
				case string(queue.StateSending):
					resp.Sending++
				case string(queue.StateFailed):
					resp.Failed++
				case string(queue.StateDeadLetter):
					resp.DeadLetter++
				}
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// SubscriptionCounter is optionally implemented by the correlation manager to report
// active in-memory subscription counts without database queries.
type SubscriptionCounter interface {
	ActiveCounts() (asks int, receives int)
}

// SubscriptionsHandler handles GET /subscriptions.
//
// It returns the count of active ask and receive correlation entries.
func SubscriptionsHandler(counter SubscriptionCounter, store persistence.CorrelationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if counter != nil {
			asks, receives := counter.ActiveCounts()
			writeJSON(w, http.StatusOK, subscriptionsResponse{
				Asks:     asks,
				Receives: receives,
			})
			return
		}

		if store != nil {
			entries, err := store.ListActive(r.Context())
			if err != nil {
				slog.Error("admin: subscriptions inspect failed", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to query subscriptions", "internal_error")
				return
			}
			var resp subscriptionsResponse
			for _, e := range entries {
				switch e.Type {
				case "ask":
					resp.Asks++
				case "receive":
					resp.Receives++
				}
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		writeJSON(w, http.StatusOK, subscriptionsResponse{})
	}
}

// ListDeadLetterHandler handles GET /queue/dead-letter.
func ListDeadLetterHandler(store persistence.QueueStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID := r.URL.Query().Get("account_id")
		limit := 50
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}

		entries, err := store.ListDeadLetter(r.Context(), accountID, limit)
		if err != nil {
			slog.Error("admin: list dead letter failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to query dead letter queue", "internal_error")
			return
		}

		out := make([]DeadLetterEntry, 0, len(entries))
		for _, e := range entries {
			out = append(out, DeadLetterEntry{
				ID:             e.ID,
				AccountID:      e.AccountID,
				RoomID:         e.RoomID,
				RetryCount:     e.RetryCount,
				LastError:      e.LastError,
				IdempotencyKey: e.IdempotencyKey,
				CreatedAt:      e.CreatedAt,
				UpdatedAt:      e.UpdatedAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// QueueRedriver allows rescheduling dead-lettered jobs for retry.
type QueueRedriver interface {
	Redrive(ctx context.Context, id string) error
	RedriveAll(ctx context.Context, accountID string) (int64, error)
}

// RedriveQueueHandler handles POST /queue/redrive/{id}.
func RedriveQueueHandler(redriver QueueRedriver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := redriver.Redrive(r.Context(), id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, err.Error(), "not_found")
				return
			}
			slog.Error("admin: queue redrive failed", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"redriven": true, "id": id})
	}
}

// RedriveAllQueueHandler handles POST /queue/redrive-all.
func RedriveAllQueueHandler(redriver QueueRedriver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID := r.URL.Query().Get("account_id")
		count, err := redriver.RedriveAll(r.Context(), accountID)
		if err != nil {
			slog.Error("admin: queue redrive all failed", "error", err)
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"redriven": true, "count": count})
	}
}
