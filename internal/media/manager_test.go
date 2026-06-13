package media

import (
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestNewAttachmentContent_UsesCaptionAsBody(t *testing.T) {
	content := newAttachmentContent("image/jpeg", "photo.jpg", 245100, Hints{
		Caption:          "Visible caption",
		FormattedCaption: "<strong>Visible caption</strong>",
		Width:            1024,
		Height:           768,
	})

	if content.MsgType != event.MsgImage {
		t.Errorf("MsgType: got %q, want %q", content.MsgType, event.MsgImage)
	}
	if content.Body != "Visible caption" {
		t.Errorf("Body: got %q", content.Body)
	}
	if content.FileName != "photo.jpg" {
		t.Errorf("FileName: got %q", content.FileName)
	}
	if content.Format != event.FormatHTML {
		t.Errorf("Format: got %q, want %q", content.Format, event.FormatHTML)
	}
	if content.FormattedBody != "<strong>Visible caption</strong>" {
		t.Errorf("FormattedBody: got %q", content.FormattedBody)
	}
	if content.Info == nil {
		t.Fatal("Info is nil")
	}
	if content.Info.MimeType != "image/jpeg" || content.Info.Size != 245100 {
		t.Errorf("Info metadata: got mime=%q size=%d", content.Info.MimeType, content.Info.Size)
	}
	if content.Info.Width != 1024 || content.Info.Height != 768 {
		t.Errorf("Info dimensions: got %dx%d", content.Info.Width, content.Info.Height)
	}
}

func TestNewAttachmentContent_FallsBackToFilenameBody(t *testing.T) {
	content := newAttachmentContent("application/pdf", "report.pdf", 2048, Hints{})

	if content.MsgType != event.MsgFile {
		t.Errorf("MsgType: got %q, want %q", content.MsgType, event.MsgFile)
	}
	if content.Body != "report.pdf" {
		t.Errorf("Body: got %q, want filename fallback", content.Body)
	}
	if content.Format != "" {
		t.Errorf("Format: got %q, want empty", content.Format)
	}
	if content.FormattedBody != "" {
		t.Errorf("FormattedBody: got %q, want empty", content.FormattedBody)
	}
}
