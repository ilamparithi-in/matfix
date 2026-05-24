package crypto

import (
	mcrypto "maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/id"

	"github.com/ilamparithi-in/matfix/internal/config"
)

// applyTrustPolicy sets the key-sharing trust thresholds on mach.
//
//   - TrustPolicyTOFU: send and share keys with cross-signed-TOFU or better
//     devices (trust-on-first-use — devices are accepted once their key is
//     observed without requiring explicit verification).
//   - TrustPolicyAllowlist: require explicitly manually-verified devices only.
//
// Any unrecognised policy falls back to TOFU behaviour.
func applyTrustPolicy(mach *mcrypto.OlmMachine, policy config.TrustPolicy) {
	switch policy {
	case config.TrustPolicyAllowlist:
		mach.SendKeysMinTrust = id.TrustStateVerified
		mach.ShareKeysMinTrust = id.TrustStateVerified
	default: // TrustPolicyTOFU and any unrecognised value
		mach.SendKeysMinTrust = id.TrustStateUnset
		mach.ShareKeysMinTrust = id.TrustStateCrossSignedTOFU
	}
}
