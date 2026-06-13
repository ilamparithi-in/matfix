package media

import (
	"context"
	"fmt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// # Config

// Hints carries optional media metadata for populating the event info block.
// Zero values are omitted from the resulting FileInfo.
// Caption overrides the Matrix body for media caption support; when empty,
// the body falls back to the filename.
// Width and Height are in pixels; Duration is in milliseconds (audio/video).
// Size overrides len(data) as the reported file size when non-zero.
type Hints struct {
	Caption          string
	FormattedCaption string
	Width            int
	Height           int
	Duration         int
	Size             int
}

// # Manager

// Manager handles media uploads (plain and encrypted) on behalf of the engine.
// It is stateless and safe for concurrent use.
//
// internal/media is a privileged package permitted to import maunium.net/go/mautrix
// directly, alongside internal/sync and internal/crypto. It must NOT import
// internal/engine.
type Manager struct {
	mx *mautrix.Client
}

// NewManager creates a Manager backed by the given mautrix client.
func NewManager(mx *mautrix.Client) *Manager {
	return &Manager{mx: mx}
}

// PrepareAttachment uploads data to the homeserver and returns a fully-populated
// MessageEventContent ready to be sent as an m.room.message event.
//
// If the room is encrypted, the attachment is encrypted in-place before upload
// and content.File is populated with the key material. For unencrypted rooms,
// content.URL is set to the plain MXC URI.
func (m *Manager) PrepareAttachment(
	ctx context.Context,
	roomID id.RoomID,
	data []byte,
	mimeType, filename string,
	hints Hints,
) (event.MessageEventContent, error) {
	encrypted, err := m.mx.StateStore.IsEncrypted(ctx, roomID)
	if err != nil {
		return event.MessageEventContent{}, fmt.Errorf("check room encryption for %s: %w", roomID, err)
	}

	size := len(data)
	if hints.Size != 0 {
		size = hints.Size
	}

	content := newAttachmentContent(mimeType, filename, size, hints)

	if encrypted {
		ciphertext, ef := EncryptAttachment(data)
		resp, err := m.mx.UploadMedia(ctx, mautrix.ReqUploadMedia{
			ContentBytes: ciphertext,
			ContentType:  "application/octet-stream",
			FileName:     filename,
		})
		if err != nil {
			return event.MessageEventContent{}, fmt.Errorf("upload encrypted attachment: %w", err)
		}
		content.File = &event.EncryptedFileInfo{
			EncryptedFile: ef,
			URL:           resp.ContentURI.CUString(),
		}
	} else {
		resp, err := m.mx.UploadMedia(ctx, mautrix.ReqUploadMedia{
			ContentBytes: data,
			ContentType:  mimeType,
			FileName:     filename,
		})
		if err != nil {
			return event.MessageEventContent{}, fmt.Errorf("upload attachment: %w", err)
		}
		content.URL = resp.ContentURI.CUString()
	}

	return content, nil
}

func newAttachmentContent(mimeType, filename string, size int, hints Hints) event.MessageEventContent {
	body := filename
	if hints.Caption != "" {
		body = hints.Caption
	}
	content := event.MessageEventContent{
		MsgType:  InferMsgType(mimeType),
		Body:     body,
		FileName: filename,
		Info: &event.FileInfo{
			MimeType: mimeType,
			Size:     size,
			Width:    hints.Width,
			Height:   hints.Height,
			Duration: hints.Duration,
		},
	}
	if hints.Caption != "" && hints.FormattedCaption != "" {
		content.Format = event.FormatHTML
		content.FormattedBody = hints.FormattedCaption
	}
	return content
}
