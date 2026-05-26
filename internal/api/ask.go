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
		// Check each explicit include room. Exclude-only or absent RoomID cannot be
		// pre-checked statically; the filter itself limits what is matched.
		if req.Filter.RoomID != nil {
			for _, rid := range req.Filter.RoomID.Include {
				if !CheckRoom(r.Context(), rid) {
					writeError(w, http.StatusForbidden, "API key does not allow room: "+rid, "forbidden")
					return
				}
			}
		}

		msg, err := payloadToSendRequest(req.Message)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "bad_request")
			return
		}

		// Send the outbound message synchronously to capture the Matrix event ID.
		// This is necessary so RegisterAsk can inject it as the InReplyTo filter,
		// ensuring we only match actual replies and never the bot's own echo.
		matrixEventID, err := sub.SendDirect(r.Context(), submission.SubmitRequest{
			AccountID:   req.AccountID,
			Destination: req.Destination,
			Message:     msg,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "submission_error")
			return
		}

		// Register the correlation ask. Use the Matrix event ID as both the
		// correlation ID and OutboundEventID so RegisterAsk auto-injects InReplyTo.
		timeout := time.Duration(req.TimeoutSeconds) * time.Second
		handle, err := cor.RegisterAsk(r.Context(), correlation.AskRequest{
			CorrelationID:   matrixEventID,
			AccountID:       req.AccountID,
			OutboundEventID: matrixEventID,
			Filter:          requestFilterToCorrelation(req.Filter),
			Timeout:         timeout,
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

		resp := apires.AskResponse{JobID: matrixEventID}
		if matched != nil {
			ep := envelopeToPayload(*matched)
			resp.MatchedEvent = &ep
		} else {
			resp.TimedOut = true
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
