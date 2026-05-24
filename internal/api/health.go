package api

import (
	"net/http"

	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/account"
)

// liveHandler returns a handler for GET /health/live.
//
// Liveness only checks whether the process is running; it always returns 200.
func liveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, apires.LivenessResponse{Status: "ok"})
	}
}

// readyHandler returns a handler for GET /health/ready.
//
// Readiness returns 200 when at least one account is available, 503 otherwise.
func readyHandler(accounts *account.AccountManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		available := accounts.Available()

		accountMap := make(map[string]string, len(accounts.All()))
		for _, actx := range accounts.All() {
			if actx.IsAvailable() {
				accountMap[actx.AccountID()] = "available"
			} else {
				accountMap[actx.AccountID()] = "failed"
			}
		}

		if len(available) == 0 {
			writeJSON(w, http.StatusServiceUnavailable, apires.ReadinessResponse{
				Status:   "not_ready",
				Accounts: accountMap,
			})
			return
		}
		writeJSON(w, http.StatusOK, apires.ReadinessResponse{
			Status:   "ready",
			Accounts: accountMap,
		})
	}
}
