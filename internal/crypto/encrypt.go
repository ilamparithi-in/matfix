package crypto

import (
	"context"
	"fmt"

	mcrypto "maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// EncryptEvent encrypts an event for the given room using Megolm.
// If no outbound Megolm session exists, ShareGroupSession is called to
// establish one (using the current room member list from the state store),
// and the encryption is then retried once.
func (m *CryptoManager) EncryptEvent(
	ctx context.Context,
	roomID id.RoomID,
	evtType event.Type,
	content interface{},
) (*event.EncryptedEventContent, error) {
	encrypted, err := m.mach.EncryptMegolmEvent(ctx, roomID, evtType, content)
	if err == nil {
		return encrypted, nil
	}
	if !mcrypto.IsShareError(err) {
		return nil, fmt.Errorf("crypto: encrypt megolm for %s: %w", roomID, err)
	}

	// No outbound session - share keys with room members then retry.
	members, err := m.stateStore.GetRoomJoinedOrInvitedMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("crypto: get room members for %s: %w", roomID, err)
	}
	if err := m.mach.ShareGroupSession(ctx, roomID, members); err != nil {
		return nil, fmt.Errorf("crypto: share group session for %s: %w", roomID, err)
	}

	encrypted, err = m.mach.EncryptMegolmEvent(ctx, roomID, evtType, content)
	if err != nil {
		return nil, fmt.Errorf("crypto: encrypt megolm for %s (after share): %w", roomID, err)
	}
	return encrypted, nil
}
