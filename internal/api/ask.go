package api

import (
	"encoding/json"
	"net/http"
	"time"

	apireq "github.com/ilamparithi-in/matfix/internal/api/request"
	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/correlation"
	"github.com/ilamparithi-in/matfix/internal/submission"
)

// askHandler returns the handler for POST /v1/ask.
//
// The handler submits an outbound message, registers a correlation ask, and
// long-polls until a matching inbound event arrives or the timeout elapses.
// The outbound event ID (returned by the homeserver) is used as the default
// InReplyTo filter when none is explicitly provided.
func askHandler(sub *submission.SubmissionManager, cor *correlation.CorrelationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apireq.AskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body", "bad_request")
			return
		}

		if req.AccountID == "" {
			writeError(w, http.StatusBadRequest, "account_id is required", "bad_request")
			return
		}
		if req.Destination == "" {
			writeError(w, http.StatusBadRequest, "destination is required", "bad_request")
			return
		}
		if req.TimeoutSeconds <= 0 {
			writeError(w, http.StatusBadRequest, "timeout_seconds must be > 0", "bad_request")
			return
		}
		if !CheckAccount(r.Context(), req.AccountID) {
			writeError(w, http.StatusForbidden, "API key does not allow account: "+req.AccountID, "forbidden")
			return
		}
		if !CheckRoom(r.Context(), req.Filter.RoomID) {
			writeError(w, http.StatusForbidden, "API key does not allow room: "+req.Filter.RoomID, "forbidden")
			return
		}

		msg, err := payloadToSendRequest(req.Message)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "bad_request")
			return
		}

		// Submit the outbound message first.
		jobID, err := sub.Submit(r.Context(), submission.SubmitRequest{
			AccountID:      req.AccountID,
			Destination:    req.Destination,
			Message:        msg,
			IdempotencyKey: req.IdempotencyKey,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "submission_error")
			return
		}

		// Register the correlation ask. The job ID doubles as the correlation ID.
		timeout := time.Duration(req.TimeoutSeconds) * time.Second
		handle, err := cor.RegisterAsk(r.Context(), correlation.AskRequest{
			CorrelationID: jobID,
			AccountID:     req.AccountID,
			Filter:        requestFilterToCorrelation(req.Filter),
			Timeout:       timeout,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "internal_error")
			return
		}

		// Block until a match arrives, timeout, or client disconnects.
		matched, err := handle.Wait(r.Context())
		if err != nil {
			writeError(w, http.StatusRequestTimeout, "request cancelled", "cancelled")
			return
		}

		resp := apires.AskResponse{JobID: jobID}
		if matched != nil {
			ep := envelopeToPayload(*matched)
			resp.MatchedEvent = &ep
		} else {
			resp.TimedOut = true
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
