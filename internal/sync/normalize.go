package sync

import (
	"context"
	"log/slog"
	"time"

	"maunium.net/go/mautrix/event"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// # Top-level dispatch

// dispatchEvent handles one timeline or state event from the sync response.
// Unknown event types are silently ignored.
func (m *SyncManager) dispatchEvent(ctx context.Context, roomID string, evt *event.Event) {
	if err := evt.Content.ParseRaw(evt.Type); err != nil {
		// ErrContentAlreadyParsed is benign; other errors may mean malformed
		// content but we still attempt type-specific handling below.
		_ = err
	}

	switch evt.Type {
	case event.EventEncrypted:
		m.dispatchEncrypted(ctx, roomID, evt)
	case event.EventMessage:
		// Skip messages sent by this account — they are outbound, not inbound.
		if evt.Sender != m.userID {
			m.dispatchMessage(ctx, roomID, evt)
		} else {
			m.markSeen(ctx, string(evt.ID))
		}
	case event.EventReaction:
		if evt.Sender != m.userID {
			m.dispatchReaction(ctx, roomID, evt)
		} else {
			m.markSeen(ctx, string(evt.ID))
		}
	case event.EventRedaction:
		if evt.Sender != m.userID {
			m.dispatchRedaction(ctx, roomID, evt)
		} else {
			m.markSeen(ctx, string(evt.ID))
		}
	case event.StateMember:
		m.dispatchMembership(ctx, roomID, evt)
	}
}

// dispatchEphemeral handles one ephemeral event (e.g. m.receipt).
func (m *SyncManager) dispatchEphemeral(ctx context.Context, roomID string, evt *event.Event) {
	if err := evt.Content.ParseRaw(evt.Type); err != nil {
		return
	}
	if evt.Type != event.EphemeralEventReceipt {
		return
	}

	content := evt.Content.AsReceipt()
	if content == nil {
		return
	}
	// ReceiptEventContent is map[EventID]Receipts; iterate safely (nil map = 0 iters).
	for eventID, receiptsByType := range *content {
		for receiptType, userReceipts := range receiptsByType {
			for userID, rr := range userReceipts {
				m.bus.Publish(bus.EventEnvelope{
					Type:      bus.EventInboundReceipt,
					AccountID: m.accountID,
					RoomID:    roomID,
					Payload: bus.InboundReceiptEvent{
						UserID:      string(userID),
						EventID:     string(eventID),
						ReceiptType: string(receiptType),
						Timestamp:   rr.Timestamp,
					},
				})
			}
		}
	}
}

// # Event-type handlers

func (m *SyncManager) dispatchEncrypted(ctx context.Context, roomID string, evt *event.Event) {
	if m.decrypter == nil {
		return
	}
	decrypted, err := m.decrypter.DecryptMegolmEvent(ctx, evt)
	if err != nil {
		slog.Warn("sync: failed to decrypt event",
			"account_id", m.accountID,
			"room_id", roomID,
			"event_id", evt.ID,
			"error", err,
		)
		m.bus.Publish(bus.EventEnvelope{
			Type:      bus.EventEncryptionFailure,
			AccountID: m.accountID,
			RoomID:    roomID,
			Payload: bus.EncryptionFailureEvent{
				EventID:  string(evt.ID),
				SenderID: string(evt.Sender),
				ErrorMsg: err.Error(),
			},
		})
		return
	}
	// Re-dispatch as if the plaintext event arrived directly.
	m.dispatchEvent(ctx, roomID, decrypted)
}

func (m *SyncManager) dispatchMessage(ctx context.Context, roomID string, evt *event.Event) {
	eventID := string(evt.ID)
	if !m.isSeen(ctx, eventID) {
		return
	}

	content := evt.Content.AsMessage()
	rel := content.RelatesTo

	if rel != nil && rel.Type == event.RelReplace {
		// Edit: the new body lives in m.new_content.
		newContent := content.NewContent
		if newContent == nil {
			return
		}
		m.bus.Publish(bus.EventEnvelope{
			Type:      bus.EventInboundEdit,
			AccountID: m.accountID,
			RoomID:    roomID,
			Payload: bus.InboundEditEvent{
				EventID:          eventID,
				SenderID:         string(evt.Sender),
				RelatesToEventID: string(rel.EventID),
				NewBody:          newContent.Body,
				NewFormattedBody: newContent.FormattedBody,
				Timestamp:        time.UnixMilli(evt.Timestamp),
			},
		})
	} else {
		// Plain message or reply.
		inReplyTo := ""
		if rel != nil {
			inReplyTo = string(rel.GetReplyTo())
		}
		var attach *bus.InboundAttachment
		switch content.MsgType {
		case event.MsgFile, event.MsgImage, event.MsgAudio, event.MsgVideo:
			attach = &bus.InboundAttachment{
				Filename: content.GetFileName(),
			}
			if content.Info != nil {
				attach.MimeType = content.Info.MimeType
				attach.Size = content.Info.Size
				attach.Width = content.Info.Width
				attach.Height = content.Info.Height
				attach.Duration = content.Info.Duration
			}
			if content.File != nil {
				attach.EncryptedFile = &bus.InboundEncryptedFile{
					URL:     string(content.File.URL),
					Key:     content.File.Key.Key,
					IV:      content.File.InitVector,
					SHA256:  content.File.Hashes.SHA256,
					Version: content.File.Version,
				}
			} else {
				attach.URL = string(content.URL)
			}
		}
		m.bus.Publish(bus.EventEnvelope{
			Type:      bus.EventInboundMessage,
			AccountID: m.accountID,
			RoomID:    roomID,
			Payload: bus.InboundMessageEvent{
				EventID:       eventID,
				SenderID:      string(evt.Sender),
				Body:          content.Body,
				FormattedBody: content.FormattedBody,
				MsgType:       string(content.MsgType),
				InReplyTo:     inReplyTo,
				Attachment:    attach,
				Timestamp:     time.UnixMilli(evt.Timestamp),
			},
		})
	}
	m.markSeen(ctx, eventID)
}

func (m *SyncManager) dispatchReaction(ctx context.Context, roomID string, evt *event.Event) {
	eventID := string(evt.ID)
	if !m.isSeen(ctx, eventID) {
		return
	}

	content := evt.Content.AsReaction()
	m.bus.Publish(bus.EventEnvelope{
		Type:      bus.EventInboundReaction,
		AccountID: m.accountID,
		RoomID:    roomID,
		Payload: bus.InboundReactionEvent{
			EventID:          eventID,
			SenderID:         string(evt.Sender),
			RelatesToEventID: string(content.RelatesTo.EventID),
			Key:              content.RelatesTo.Key,
			Timestamp:        time.UnixMilli(evt.Timestamp),
		},
	})
	m.markSeen(ctx, eventID)
}

func (m *SyncManager) dispatchRedaction(ctx context.Context, roomID string, evt *event.Event) {
	eventID := string(evt.ID)
	if !m.isSeen(ctx, eventID) {
		return
	}

	// Redacted event ID may be at the top-level field (older room versions)
	// or inside the content (room version 11+).
	redacts := string(evt.Redacts)
	content := evt.Content.AsRedaction()
	if redacts == "" {
		redacts = string(content.Redacts)
	}

	m.bus.Publish(bus.EventEnvelope{
		Type:      bus.EventInboundRedaction,
		AccountID: m.accountID,
		RoomID:    roomID,
		Payload: bus.InboundRedactionEvent{
			EventID:   eventID,
			SenderID:  string(evt.Sender),
			Redacts:   redacts,
			Reason:    content.Reason,
			Timestamp: time.UnixMilli(evt.Timestamp),
		},
	})
	m.markSeen(ctx, eventID)
}

func (m *SyncManager) dispatchMembership(ctx context.Context, roomID string, evt *event.Event) {
	if evt.StateKey == nil {
		return
	}
	eventID := string(evt.ID)
	if !m.isSeen(ctx, eventID) {
		return
	}

	content := evt.Content.AsMember()

	prevMembership := ""
	if evt.Unsigned.PrevContent != nil {
		if err := evt.Unsigned.PrevContent.ParseRaw(event.StateMember); err == nil {
			prevMembership = string(evt.Unsigned.PrevContent.AsMember().Membership)
		}
	}

	m.bus.Publish(bus.EventEnvelope{
		Type:      bus.EventInboundMembership,
		AccountID: m.accountID,
		RoomID:    roomID,
		Payload: bus.InboundMembershipEvent{
			EventID:        eventID,
			UserID:         *evt.StateKey,
			Membership:     string(content.Membership),
			PrevMembership: prevMembership,
			Timestamp:      time.UnixMilli(evt.Timestamp),
		},
	})
	m.markSeen(ctx, eventID)
}
