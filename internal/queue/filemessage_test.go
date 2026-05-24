package queue_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ilamparithi-in/matfix/internal/engine"
)

func TestPayloadRoundTrip_FileMessage(t *testing.T) {
	ctx := context.Background()
	mgr := newManager(t, 3)

	want := engine.FileMessage{
		Filename: "photo.png",
		MimeType: "image/png",
		Data:     []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, // PNG header
		Width:    640,
		Height:   480,
		Size:     8,
	}

	job, err := mgr.Enqueue(ctx, "acc1", "!room:server", want, "fm-idem-1")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	pulled, err := mgr.PullNext(ctx, "acc1")
	if err != nil {
		t.Fatalf("PullNext: %v", err)
	}
	if pulled == nil {
		t.Fatal("PullNext returned nil")
	}
	if pulled.ID != job.ID {
		t.Fatalf("pulled wrong job: want %s, got %s", job.ID, pulled.ID)
	}

	got, ok := pulled.Request.(engine.FileMessage)
	if !ok {
		t.Fatalf("Request is %T, want engine.FileMessage", pulled.Request)
	}
	if got.Filename != want.Filename {
		t.Errorf("Filename: want %q, got %q", want.Filename, got.Filename)
	}
	if got.MimeType != want.MimeType {
		t.Errorf("MimeType: want %q, got %q", want.MimeType, got.MimeType)
	}
	if !bytes.Equal(got.Data, want.Data) {
		t.Errorf("Data: want %v, got %v", want.Data, got.Data)
	}
	if got.Width != want.Width {
		t.Errorf("Width: want %d, got %d", want.Width, got.Width)
	}
	if got.Height != want.Height {
		t.Errorf("Height: want %d, got %d", want.Height, got.Height)
	}
	if got.Size != want.Size {
		t.Errorf("Size: want %d, got %d", want.Size, got.Size)
	}
}
