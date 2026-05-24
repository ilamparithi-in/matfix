package api

import (
	"net/http"
	"time"

	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/account"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/queue"
)

// adminQueueHandler returns the handler for GET /v1/admin/queue.
//
// It reports the count of jobs in each significant queue state across all
// configured accounts.
func adminQueueHandler(accounts *account.AccountManager, store persistence.QueueStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all := accounts.All()

		var queued, sending, failed, deadLetter int
		for _, actx := range all {
			entries, err := store.ListByState(r.Context(), actx.AccountID(), []string{
				string(queue.StateQueued), string(queue.StateSending),
				string(queue.StateFailed), string(queue.StateDeadLetter),
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to query queue", "internal_error")
				return
			}
			for _, e := range entries {
				switch e.State {
				case string(queue.StateQueued):
					queued++
				case string(queue.StateSending):
					sending++
				case string(queue.StateFailed):
					failed++
				case string(queue.StateDeadLetter):
					deadLetter++
				}
			}
		}

		writeJSON(w, http.StatusOK, apires.AdminQueueResponse{
			Queued:     queued,
			Sending:    sending,
			Failed:     failed,
			DeadLetter: deadLetter,
		})
	}
}

// adminAccountsHandler returns the handler for GET /v1/admin/accounts.
func adminAccountsHandler(accounts *account.AccountManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all := accounts.All()
		statuses := make([]apires.AccountStatus, 0, len(all))
		for _, actx := range all {
			s := apires.AccountStatus{
				ID:        actx.AccountID(),
				Available: actx.IsAvailable(),
			}
			if err := actx.Err(); err != nil {
				s.Error = err.Error()
			}
			statuses = append(statuses, s)
		}
		writeJSON(w, http.StatusOK, apires.AdminAccountsResponse{Accounts: statuses})
	}
}

// adminSubscriptionsHandler returns the handler for GET /v1/admin/subscriptions.
func adminSubscriptionsHandler(store persistence.CorrelationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := store.ListActive(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query subscriptions", "internal_error")
			return
		}

		var asks, receives int
		for _, e := range entries {
			// Skip entries that have already timed out at the DB level.
			if e.TimeoutAt > 0 && e.TimeoutAt < time.Now().UnixMilli() {
				continue
			}
			switch e.Type {
			case "ask":
				asks++
			case "receive":
				receives++
			}
		}

		writeJSON(w, http.StatusOK, apires.AdminSubscriptionsResponse{
			Asks:     asks,
			Receives: receives,
		})
	}
}
