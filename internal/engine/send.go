package engine

import (
	"context"
	"fmt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// # Send

// Send dispatches req to roomID and returns the resulting Matrix event ID.
//
// Encryption is handled transparently by the underlying mautrix.Client when a
// CryptoHelper has been set on it (see Client.Underlying().Crypto). All other
// modules must call Send rather than the mautrix client directly.
func (c *Client) Send(ctx context.Context, roomID id.RoomID, req SendRequest) (id.EventID, error) {
	switch r := req.(type) {

	case TextMessage:
		return c.sendContent(ctx, roomID, event.EventMessage, &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    r.Body,
		})

	case HTMLMessage:
		return c.sendContent(ctx, roomID, event.EventMessage, &event.MessageEventContent{
			MsgType:       event.MsgText,
			Body:          r.Body,
			Format:        event.FormatHTML,
			FormattedBody: SanitizeHTML(r.FormattedBody),
		})

	case Reply:
		content := &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    r.Body,
		}
		if r.FormattedBody != "" {
			content.Format = event.FormatHTML
			content.FormattedBody = SanitizeHTML(r.FormattedBody)
		}
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(r.InReplyTo)
		return c.sendContent(ctx, roomID, event.EventMessage, content)

	case Reaction:
		return c.sendContent(ctx, roomID, event.EventReaction, &event.ReactionEventContent{
			RelatesTo: event.RelatesTo{
				Type:    event.RelAnnotation,
				EventID: r.TargetEventID,
				Key:     r.Key,
			},
		})

	case Edit:
		content := &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    r.NewBody,
		}
		if r.NewFormattedBody != "" {
			content.Format = event.FormatHTML
			content.FormattedBody = SanitizeHTML(r.NewFormattedBody)
		}
		// SetEdit wraps the content in m.new_content and adds the m.replace
		// relation; it also prepends "* " to the fallback body/formatted_body.
		content.SetEdit(r.TargetEventID)
		return c.sendContent(ctx, roomID, event.EventMessage, content)

	case Redaction:
		resp, err := c.mx.RedactEvent(ctx, roomID, r.TargetEventID, mautrix.ReqRedact{
			Reason: r.Reason,
		})
		if err != nil {
			return "", fmt.Errorf("redact %s in %s: %w", r.TargetEventID, roomID, err)
		}
		return resp.EventID, nil

	default:
		return "", fmt.Errorf("unknown send request type %T", req)
	}
}

// sendContent calls SendMessageEvent and returns the resulting event ID.
func (c *Client) sendContent(ctx context.Context, roomID id.RoomID, evtType event.Type, content any) (id.EventID, error) {
	resp, err := c.mx.SendMessageEvent(ctx, roomID, evtType, content)
	if err != nil {
		return "", fmt.Errorf("send %s to %s: %w", evtType, roomID, err)
	}
	return resp.EventID, nil
}
