package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// jobStatusHandler returns the handler for GET /v1/jobs/{job_id}.
func jobStatusHandler(queueStore persistence.QueueStore, corrStore persistence.CorrelationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := chi.URLParam(r, "job_id")
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "job_id is required", "bad_request")
			return
		}

		perms := permissionsFromCtx(r.Context())
		if perms == nil {
			writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}

		// 1. Try to fetch from the outbound queue (send jobs).
		qEntry, err := queueStore.GetByID(r.Context(), jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}

		if qEntry != nil {
			if !routeAllowed(perms, "jobs") && !routeAllowed(perms, "send") {
				writeError(w, http.StatusForbidden, "API key does not allow route: jobs or send", "forbidden")
				return
			}
			if !CheckAccount(r.Context(), qEntry.AccountID) {
				writeError(w, http.StatusForbidden, "API key does not allow account: "+qEntry.AccountID, "forbidden")
				return
			}

			resp := apires.JobStatusResponse{
				JobID:         qEntry.ID,
				Type:          "send",
				AccountID:     qEntry.AccountID,
				State:         qEntry.State,
				RetryCount:    qEntry.RetryCount,
				MatrixEventID: qEntry.MatrixEventID,
				CreatedAt:     qEntry.CreatedAt,
				UpdatedAt:     qEntry.UpdatedAt,
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		// 2. Try to fetch from correlation state (ask and receive jobs).
		cEntry, err := corrStore.GetByID(r.Context(), jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}

		if cEntry != nil {
			if !routeAllowed(perms, "jobs") && !routeAllowed(perms, cEntry.Type) {
				writeError(w, http.StatusForbidden, "API key does not allow route: jobs or "+cEntry.Type, "forbidden")
				return
			}
			if !CheckAccount(r.Context(), cEntry.AccountID) {
				writeError(w, http.StatusForbidden, "API key does not allow account: "+cEntry.AccountID, "forbidden")
				return
			}

			var resolvedEventIDs []string
			if cEntry.ResolvedEventIDs != "" {
				if err := json.Unmarshal([]byte(cEntry.ResolvedEventIDs), &resolvedEventIDs); err != nil {
					// Fallback to empty list if unmarshal fails.
					resolvedEventIDs = []string{}
				}
			}

			resp := apires.JobStatusResponse{
				JobID:            cEntry.ID,
				Type:             cEntry.Type,
				AccountID:        cEntry.AccountID,
				State:            cEntry.State,
				MatrixEventID:    cEntry.OutboundEventID, // For ask, this is the outbound event ID.
				ResolvedEventIDs: resolvedEventIDs,
				CreatedAt:        cEntry.CreatedAt,
				UpdatedAt:        cEntry.UpdatedAt,
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		// 3. Not found in either.
		writeError(w, http.StatusNotFound, "job not found", "not_found")
	}
}
