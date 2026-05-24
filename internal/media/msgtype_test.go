package media

import (
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestInferMsgType(t *testing.T) {
	tests := []struct {
		mimeType string
		want     event.MessageType
	}{
		{"image/png", event.MsgImage},
		{"image/jpeg", event.MsgImage},
		{"image/gif", event.MsgImage},
		{"audio/mpeg", event.MsgAudio},
		{"audio/ogg", event.MsgAudio},
		{"video/mp4", event.MsgVideo},
		{"video/webm", event.MsgVideo},
		{"application/pdf", event.MsgFile},
		{"application/zip", event.MsgFile},
		{"text/plain", event.MsgFile},
		{"", event.MsgFile},
	}
	for _, tc := range tests {
		t.Run(tc.mimeType, func(t *testing.T) {
			got := InferMsgType(tc.mimeType)
			if got != tc.want {
				t.Errorf("InferMsgType(%q) = %q, want %q", tc.mimeType, got, tc.want)
			}
		})
	}
}
