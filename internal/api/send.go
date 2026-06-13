package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"maunium.net/go/mautrix/id"

	apireq "github.com/ilamparithi-in/matfix/internal/api/request"
	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/submission"
)

// sendHandler returns the handler for POST /v1/send.
func sendHandler(mgr *submission.SubmissionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apireq.SendRequest
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
		if !CheckAccount(r.Context(), req.AccountID) {
			writeError(w, http.StatusForbidden, "API key does not allow account: "+req.AccountID, "forbidden")
			return
		}

		msg, err := payloadToSendRequest(req.Message)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "bad_request")
			return
		}

		jobID, err := mgr.Submit(r.Context(), submission.SubmitRequest{
			AccountID:      req.AccountID,
			Destination:    req.Destination,
			Message:        msg,
			IdempotencyKey: req.IdempotencyKey,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "submission_error")
			return
		}

		writeJSON(w, http.StatusAccepted, apires.SendResponse{JobID: jobID})
	}
}

// payloadToSendRequest converts a JSON MessagePayload to an engine.SendRequest.
func payloadToSendRequest(p apireq.MessagePayload) (engine.SendRequest, error) {
	switch p.Type {
	case "text":
		return engine.TextMessage{Body: p.Body}, nil
	case "html":
		return engine.HTMLMessage{Body: p.Body, FormattedBody: p.FormattedBody}, nil
	case "reply":
		return engine.Reply{
			InReplyTo:     id.EventID(p.InReplyTo),
			Body:          p.Body,
			FormattedBody: p.FormattedBody,
		}, nil
	case "reaction":
		return engine.Reaction{
			TargetEventID: id.EventID(p.TargetEventID),
			Key:           p.Key,
		}, nil
	case "edit":
		return engine.Edit{
			TargetEventID:    id.EventID(p.TargetEventID),
			NewBody:          p.Body,
			NewFormattedBody: p.FormattedBody,
		}, nil
	case "redaction":
		return engine.Redaction{
			TargetEventID: id.EventID(p.TargetEventID),
			Reason:        p.Reason,
		}, nil
	case "file":
		if p.File == nil {
			return nil, fmt.Errorf("message type \"file\" requires a file attachment")
		}
		if p.FormattedBody != "" && p.Body == "" {
			return nil, fmt.Errorf("file attachment formatted_body requires body")
		}
		if p.File.MimeType == "" {
			return nil, fmt.Errorf("file attachment requires mime_type")
		}
		if p.File.Data == "" {
			return nil, fmt.Errorf("file attachment requires data")
		}
		raw, err := base64.StdEncoding.DecodeString(p.File.Data)
		if err != nil {
			return nil, fmt.Errorf("file attachment data: invalid base64: %w", err)
		}
		const maxFileSize = 52_428_800 // 50 MiB
		if len(raw) > maxFileSize {
			return nil, fmt.Errorf("file attachment exceeds 50 MiB limit (%d bytes decoded)", len(raw))
		}
		return engine.FileMessage{
			Filename:         p.File.Filename,
			MimeType:         p.File.MimeType,
			Data:             raw,
			Caption:          p.Body,
			FormattedCaption: p.FormattedBody,
			Width:            p.File.Width,
			Height:           p.File.Height,
			Duration:         p.File.Duration,
		}, nil
	default:
		return nil, fmt.Errorf("unknown message type: %q", p.Type)
	}
}
