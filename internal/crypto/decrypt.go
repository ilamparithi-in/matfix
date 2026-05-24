package crypto

import (
	"context"
	"log/slog"

	"maunium.net/go/mautrix/event"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// DecryptMegolmEvent decrypts an m.room.encrypted event.
// On failure an EncryptionFailureEvent is published to the bus and the error
// is returned. This method never panics.
//
// It satisfies the sync.Decrypter interface.
func (m *CryptoManager) DecryptMegolmEvent(ctx context.Context, evt *event.Event) (*event.Event, error) {
	decrypted, err := m.mach.DecryptMegolmEvent(ctx, evt)
	if err != nil {
		slog.ErrorContext(ctx, "megolm decryption failed",
			"account_id", m.accountID,
			"event_id", evt.ID,
			"sender", evt.Sender,
			"error", err,
		)
		m.bus.Publish(bus.EventEnvelope{
			Type:      bus.EventEncryptionFailure,
			AccountID: m.accountID,
			RoomID:    string(evt.RoomID),
			Payload: bus.EncryptionFailureEvent{
				EventID:  string(evt.ID),
				SenderID: string(evt.Sender),
				ErrorMsg: err.Error(),
			},
		})
		return nil, err
	}
	return decrypted, nil
}
