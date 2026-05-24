package crypto

import (
	"context"

	"maunium.net/go/mautrix/id"
)

// DeviceTrust returns the recorded trust state for the given user's device.
// Returns id.TrustStateUnknownDevice when the device is not in the crypto store
// or a store error occurs.
func (m *CryptoManager) DeviceTrust(ctx context.Context, userID id.UserID, deviceID id.DeviceID) id.TrustState {
	device, err := m.mach.CryptoStore.GetDevice(ctx, userID, deviceID)
	if err != nil || device == nil {
		return id.TrustStateUnknownDevice
	}
	return device.Trust
}
