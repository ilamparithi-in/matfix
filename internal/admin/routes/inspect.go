package routes

import (
	"log/slog"
	"net/http"

	"github.com/ilamparithi-in/matfix/internal/account"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/queue"
)

// # Response types

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

// SubscriptionsHandler handles GET /subscriptions.
//
// It returns the count of active ask and receive correlation entries.
func SubscriptionsHandler(store persistence.CorrelationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}
