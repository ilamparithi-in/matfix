package api

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	apireq "github.com/ilamparithi-in/matfix/internal/api/request"
	"github.com/ilamparithi-in/matfix/internal/engine"
)

func TestPayloadToSendRequest_FileCaption(t *testing.T) {
	raw := []byte("image bytes")
	req, err := payloadToSendRequest(apireq.MessagePayload{
		Type:          "file",
		Body:          "Visible caption",
		FormattedBody: "<b>Visible caption</b>",
		File: &apireq.FileAttachment{
			Data:     base64.StdEncoding.EncodeToString(raw),
			MimeType: "image/jpeg",
			Filename: "photo.jpg",
			Width:    1024,
			Height:   768,
		},
	})
	if err != nil {
		t.Fatalf("payloadToSendRequest: %v", err)
	}

	got, ok := req.(engine.FileMessage)
	if !ok {
		t.Fatalf("req is %T, want engine.FileMessage", req)
	}
	if got.Caption != "Visible caption" {
		t.Errorf("Caption: got %q", got.Caption)
	}
	if got.FormattedCaption != "<b>Visible caption</b>" {
		t.Errorf("FormattedCaption: got %q", got.FormattedCaption)
	}
	if !bytes.Equal(got.Data, raw) {
		t.Errorf("Data: got %v, want %v", got.Data, raw)
	}
	if got.Filename != "photo.jpg" || got.MimeType != "image/jpeg" {
		t.Errorf("file metadata: got filename=%q mime_type=%q", got.Filename, got.MimeType)
	}
	if got.Width != 1024 || got.Height != 768 {
		t.Errorf("dimensions: got %dx%d", got.Width, got.Height)
	}
}

func TestPayloadToSendRequest_FileFormattedCaptionRequiresBody(t *testing.T) {
	_, err := payloadToSendRequest(apireq.MessagePayload{
		Type:          "file",
		FormattedBody: "<b>caption</b>",
		File: &apireq.FileAttachment{
			Data:     base64.StdEncoding.EncodeToString([]byte("data")),
			MimeType: "image/png",
			Filename: "image.png",
		},
	})
	if err == nil {
		t.Fatal("payloadToSendRequest returned nil error")
	}
	if !strings.Contains(err.Error(), "formatted_body requires body") {
		t.Fatalf("error = %q, want formatted_body validation", err)
	}
}
