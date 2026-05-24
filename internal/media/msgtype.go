package media

import (
	"strings"

	"maunium.net/go/mautrix/event"
)

// InferMsgType returns the Matrix message type for the given MIME type.
// image/* → MsgImage, audio/* → MsgAudio, video/* → MsgVideo, else → MsgFile.
func InferMsgType(mimeType string) event.MessageType {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return event.MsgImage
	case strings.HasPrefix(mimeType, "audio/"):
		return event.MsgAudio
	case strings.HasPrefix(mimeType, "video/"):
		return event.MsgVideo
	default:
		return event.MsgFile
	}
}
