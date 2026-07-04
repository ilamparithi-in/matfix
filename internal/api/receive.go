package api

import (
	"encoding/json"
	"net/http"
	"time"

	apireq "github.com/ilamparithi-in/matfix/internal/api/request"
	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/correlation"
)

// receiveHandler returns the handler for POST /v1/receive.
//
// The handler registers a receive subscription and long-polls until the window
// closes (timeout or event limit) or the client disconnects. The response is
// always 200 OK; TimedOut=true indicates the window closed without any events.
func receiveHandler(mgr *correlation.CorrelationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apireq.ReceiveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body", "bad_request")
			return
		}

		if req.AccountID == "" {
			writeError(w, http.StatusBadRequest, "account_id is required", "bad_request")
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
		// pre-checked statically; the filter itself limits what is collected.
		if req.Filter.RoomID != nil {
			for _, rid := range req.Filter.RoomID.Include {
				if !CheckRoom(r.Context(), rid) {
					writeError(w, http.StatusForbidden, "API key does not allow room: "+rid, "forbidden")
					return
				}
			}
		}

		timeout := time.Duration(req.TimeoutSeconds) * time.Second
		handle, err := mgr.RegisterReceive(r.Context(), correlation.ReceiveRequest{
			AccountID: req.AccountID,
			Filter:    requestFilterToCorrelation(req.Filter),
			Timeout:   timeout,
			Limit:     req.Limit,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "bad_request")
			return
		}

		events, err := handle.Wait(r.Context())
		if err != nil {
			// Client disconnected or context cancelled.
			writeError(w, http.StatusRequestTimeout, "request cancelled", "cancelled")
			return
		}

		resp := apires.ReceiveResponse{
			JobID:    handle.ID,
			Events:   envelopesToPayloads(events),
			TimedOut: len(events) == 0,
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// requestFilterToCorrelation recursively maps an API FilterNode to a correlation.FilterNode.
func requestFilterToCorrelation(f apireq.FilterNode) correlation.FilterNode {
	n := correlation.FilterNode{
		InReplyTo:        f.InReplyTo,
		BodyRegex:        f.BodyRegex,
		ReactionKey:      f.ReactionKey,
		RelatesToEventID: f.RelatesToEventID,
		HasAttachment:    f.HasAttachment,
		MinTimestamp:     f.MinTimestamp,
		MaxTimestamp:     f.MaxTimestamp,
	}
	if f.SenderID != nil {
		n.SenderID = &correlation.StringSetFilter{Include: f.SenderID.Include, Exclude: f.SenderID.Exclude}
	}
	if f.RoomID != nil {
		n.RoomID = &correlation.StringSetFilter{Include: f.RoomID.Include, Exclude: f.RoomID.Exclude}
	}
	if f.EventType != nil {
		n.EventType = &correlation.StringSetFilter{Include: f.EventType.Include, Exclude: f.EventType.Exclude}
	}
	for _, child := range f.AllOf {
		cc := requestFilterToCorrelation(*child)
		n.AllOf = append(n.AllOf, &cc)
	}
	for _, child := range f.AnyOf {
		cc := requestFilterToCorrelation(*child)
		n.AnyOf = append(n.AnyOf, &cc)
	}
	if f.Not != nil {
		cc := requestFilterToCorrelation(*f.Not)
		n.Not = &cc
	}
	return n
}

// envelopesToPayloads converts a slice of bus.EventEnvelope to response payloads.
func envelopesToPayloads(envs []bus.EventEnvelope) []apires.EventPayload {
	if len(envs) == 0 {
		return []apires.EventPayload{}
	}
	out := make([]apires.EventPayload, 0, len(envs))
	for _, env := range envs {
		out = append(out, envelopeToPayload(env))
	}
	return out
}

// envelopeToPayload converts one bus.EventEnvelope to a response EventPayload.
func envelopeToPayload(env bus.EventEnvelope) apires.EventPayload {
	p := apires.EventPayload{
		AccountID: env.AccountID,
		RoomID:    env.RoomID,
		Type:      string(env.Type),
	}
	switch evt := env.Payload.(type) {
	case bus.InboundMessageEvent:
		p.EventID = evt.EventID
		p.SenderID = evt.SenderID
		p.Body = evt.Body
		p.Timestamp = evt.Timestamp.UnixMilli()
		if evt.Attachment != nil {
			ap := &apires.AttachmentPayload{
				URL:      evt.Attachment.URL,
				MimeType: evt.Attachment.MimeType,
				Filename: evt.Attachment.Filename,
				Size:     evt.Attachment.Size,
				Width:    evt.Attachment.Width,
				Height:   evt.Attachment.Height,
				Duration: evt.Attachment.Duration,
			}
			if evt.Attachment.EncryptedFile != nil {
				ap.EncryptedFile = &apires.EncryptedFilePayload{
					URL:     evt.Attachment.EncryptedFile.URL,
					Key:     evt.Attachment.EncryptedFile.Key,
					IV:      evt.Attachment.EncryptedFile.IV,
					SHA256:  evt.Attachment.EncryptedFile.SHA256,
					Version: evt.Attachment.EncryptedFile.Version,
				}
			}
			p.Attachment = ap
		}
	case bus.InboundReactionEvent:
		p.EventID = evt.EventID
		p.SenderID = evt.SenderID
		p.Body = evt.Key
		p.Timestamp = evt.Timestamp.UnixMilli()
	case bus.InboundEditEvent:
		p.EventID = evt.EventID
		p.SenderID = evt.SenderID
		p.Body = evt.NewBody
		p.Timestamp = evt.Timestamp.UnixMilli()
	case bus.InboundRedactionEvent:
		p.EventID = evt.EventID
		p.SenderID = evt.SenderID
		p.Body = evt.Reason
		p.Timestamp = evt.Timestamp.UnixMilli()
	case bus.InboundMembershipEvent:
		p.EventID = evt.EventID
		p.SenderID = evt.UserID
		p.Body = evt.Membership
		p.Timestamp = evt.Timestamp.UnixMilli()
	case bus.InboundReceiptEvent:
		p.EventID = evt.EventID
		p.SenderID = evt.UserID
		p.Timestamp = evt.Timestamp.UnixMilli()
	}
	return p
}
