package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ilamparithi-in/matfix/internal/webhook"
)

// ListWebhooksHandler returns the handler for GET /webhooks.
func ListWebhooksHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeJSON(w, http.StatusOK, []webhook.WebhookRule{})
			return
		}
		writeJSON(w, http.StatusOK, mgr.List())
	}
}

// GetWebhookHandler returns the handler for GET /webhooks/{id}.
func GetWebhookHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusNotFound, "webhooks disabled", "not_found")
			return
		}
		id := chi.URLParam(r, "id")
		rule := mgr.Get(id)
		if rule == nil {
			writeError(w, http.StatusNotFound, "webhook not found", "not_found")
			return
		}
		writeJSON(w, http.StatusOK, rule)
	}
}

// CreateWebhookHandler returns the handler for POST /webhooks.
func CreateWebhookHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusBadRequest, "webhooks disabled", "bad_request")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var rule webhook.WebhookRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "bad_request")
			return
		}

		if err := mgr.AddOrUpdate(rule); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "bad_request")
			return
		}

		writeJSON(w, http.StatusOK, rule)
	}
}

// DeleteWebhookHandler returns the handler for DELETE /webhooks/{id}.
func DeleteWebhookHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusBadRequest, "webhooks disabled", "bad_request")
			return
		}

		id := chi.URLParam(r, "id")
		deleted, err := mgr.Delete(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}
		if !deleted {
			writeError(w, http.StatusNotFound, "webhook not found", "not_found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
	}
}

// ReloadWebhooksHandler returns the handler for POST /webhooks/reload.
func ReloadWebhooksHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusBadRequest, "webhooks disabled", "bad_request")
			return
		}

		if err := mgr.Reload(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"reloaded": true, "count": len(mgr.List())})
	}
}

// WebhookQueueResponse reports webhook queue metrics.
type WebhookQueueResponse struct {
	Failed     int `json:"failed"`
	Sending    int `json:"sending"`
	DeadLetter int `json:"dead_letter"`
}

// WebhookQueueHandler returns the handler for GET /webhooks/queue.
func WebhookQueueHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeJSON(w, http.StatusOK, WebhookQueueResponse{})
			return
		}

		failed, sending, deadLetter, err := mgr.QueueStats(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}

		writeJSON(w, http.StatusOK, WebhookQueueResponse{
			Failed:     failed,
			Sending:    sending,
			DeadLetter: deadLetter,
		})
	}
}

// RedriveWebhookHandler returns the handler for POST /webhooks/queue/redrive/{id}.
func RedriveWebhookHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusBadRequest, "webhooks disabled", "bad_request")
			return
		}

		id := chi.URLParam(r, "id")
		if err := mgr.Redrive(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"redriven": true, "id": id})
	}
}

// RedriveAllWebhooksHandler returns the handler for POST /webhooks/queue/redrive-all.
func RedriveAllWebhooksHandler(mgr *webhook.WebhookManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusBadRequest, "webhooks disabled", "bad_request")
			return
		}

		count, err := mgr.RedriveAll(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"redriven": true, "count": count})
	}
}
