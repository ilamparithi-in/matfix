package sync

import (
	"context"
	"encoding/json"
	"testing"

	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # Test doubles

type stubCacheStore struct{}

func (s *stubCacheStore) Has(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (s *stubCacheStore) Insert(_ context.Context, _ persistence.EventCacheEntry) error {
	return nil
}
func (s *stubCacheStore) Prune(_ context.Context, _ int64) (int64, error) { return 0, nil }

type captureBus struct {
	published []bus.EventEnvelope
}

func (b *captureBus) Publish(env bus.EventEnvelope) {
	b.published = append(b.published, env)
}
func (b *captureBus) Subscribe(_ bus.EventType, _ bus.HandlerFunc) bus.SubscriptionID { return "" }
func (b *captureBus) Unsubscribe(_ bus.SubscriptionID)                                {}

func (b *captureBus) lastMessage() (bus.InboundMessageEvent, bool) {
	for i := len(b.published) - 1; i >= 0; i-- {
		if msg, ok := b.published[i].Payload.(bus.InboundMessageEvent); ok {
			return msg, true
		}
	}
	return bus.InboundMessageEvent{}, false
}

// newTestManager builds a SyncManager with stub dependencies.
// client, syncStore, decrypter and cancel are left nil — dispatchMessage does not use them.
func newTestManager(b *captureBus) *SyncManager {
	return &SyncManager{
		accountID:  "test-account",
		cacheStore: &stubCacheStore{},
		bus:        b,
	}
}

// makeMessageEvent builds a fully parsed mautrix event for content by
// marshalling to JSON then running ParseRaw, mirroring what the real sync
// pipeline does.
func makeMessageEvent(evtID, sender string, content event.MessageEventContent) *event.Event {
	raw, err := json.Marshal(content)
	if err != nil {
		panic("makeMessageEvent marshal: " + err.Error())
	}
	evt := &event.Event{
		ID:        id.EventID(evtID),
		Sender:    id.UserID(sender),
		Type:      event.EventMessage,
		Timestamp: 1700000000000,
	}
	if err := json.Unmarshal(raw, &evt.Content); err != nil {
		panic("makeMessageEvent unmarshal: " + err.Error())
	}
	_ = evt.Content.ParseRaw(event.EventMessage)
	return evt
}

// # Tests

func TestDispatchMessage_PlainAttachment(t *testing.T) {
	tests := []struct {
		msgType  event.MessageType
		mimeType string
		url      string
	}{
		{event.MsgFile, "application/pdf", "mxc://server/file1"},
		{event.MsgImage, "image/png", "mxc://server/img1"},
		{event.MsgAudio, "audio/ogg", "mxc://server/aud1"},
		{event.MsgVideo, "video/mp4", "mxc://server/vid1"},
	}

	for _, tc := range tests {
		t.Run(string(tc.msgType), func(t *testing.T) {
			b := &captureBus{}
			m := newTestManager(b)

			evt := makeMessageEvent("$ev1:server", "@user:server", event.MessageEventContent{
				MsgType:  tc.msgType,
				Body:     "file.bin",
				FileName: "file.bin",
				URL:      id.ContentURIString(tc.url),
				Info: &event.FileInfo{
					MimeType: tc.mimeType,
					Size:     4096,
					Width:    320,
					Height:   240,
					Duration: 5000,
				},
			})

			m.dispatchMessage(context.Background(), "!room:server", evt)

			msg, ok := b.lastMessage()
			if !ok {
				t.Fatal("no InboundMessageEvent published")
			}
			if msg.Attachment == nil {
				t.Fatalf("Attachment is nil for msgtype %s", tc.msgType)
			}
			att := msg.Attachment
			if att.URL != tc.url {
				t.Errorf("URL: got %q, want %q", att.URL, tc.url)
			}
			if att.MimeType != tc.mimeType {
				t.Errorf("MimeType: got %q, want %q", att.MimeType, tc.mimeType)
			}
			if att.Size != 4096 {
				t.Errorf("Size: got %d, want 4096", att.Size)
			}
			if att.Width != 320 {
				t.Errorf("Width: got %d, want 320", att.Width)
			}
			if att.Height != 240 {
				t.Errorf("Height: got %d, want 240", att.Height)
			}
			if att.Duration != 5000 {
				t.Errorf("Duration: got %d, want 5000", att.Duration)
			}
			if att.EncryptedFile != nil {
				t.Error("EncryptedFile should be nil for plain attachment")
			}
		})
	}
}

func TestDispatchMessage_EncryptedAttachment(t *testing.T) {
	ef := attachment.NewEncryptedFile()
	// EncryptInPlace populates Hashes.SHA256; without it the hash is empty.
	ef.EncryptInPlace([]byte("fake image data"))

	b := &captureBus{}
	m := newTestManager(b)

	evt := makeMessageEvent("$ev2:server", "@user:server", event.MessageEventContent{
		MsgType:  event.MsgImage,
		Body:     "photo.jpg",
		FileName: "photo.jpg",
		File: &event.EncryptedFileInfo{
			EncryptedFile: *ef,
			URL:           "mxc://server/enc1",
		},
		Info: &event.FileInfo{MimeType: "image/jpeg", Size: 2048},
	})

	m.dispatchMessage(context.Background(), "!room:server", evt)

	msg, ok := b.lastMessage()
	if !ok {
		t.Fatal("no InboundMessageEvent published")
	}
	if msg.Attachment == nil {
		t.Fatal("Attachment is nil for encrypted image")
	}
	att := msg.Attachment
	if att.URL != "" {
		t.Errorf("URL should be empty for encrypted attachment, got %q", att.URL)
	}
	if att.EncryptedFile == nil {
		t.Fatal("EncryptedFile is nil for encrypted attachment")
	}
	enc := att.EncryptedFile
	if enc.URL != "mxc://server/enc1" {
		t.Errorf("EncryptedFile.URL: got %q, want %q", enc.URL, "mxc://server/enc1")
	}
	if enc.Key == "" {
		t.Error("EncryptedFile.Key is empty")
	}
	if enc.IV == "" {
		t.Error("EncryptedFile.IV is empty")
	}
	if enc.SHA256 == "" {
		t.Error("EncryptedFile.SHA256 is empty — key material not propagated")
	}
	if enc.Version == "" {
		t.Error("EncryptedFile.Version is empty")
	}
}

func TestDispatchMessage_NoAttachmentForText(t *testing.T) {
	b := &captureBus{}
	m := newTestManager(b)

	evt := makeMessageEvent("$ev3:server", "@user:server", event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    "hello",
	})

	m.dispatchMessage(context.Background(), "!room:server", evt)

	msg, ok := b.lastMessage()
	if !ok {
		t.Fatal("no InboundMessageEvent published")
	}
	if msg.Attachment != nil {
		t.Errorf("Attachment should be nil for m.text, got %+v", msg.Attachment)
	}
}
