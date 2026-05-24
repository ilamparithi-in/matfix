package crypto

import (
	"context"
	"errors"
	"log/slog"

	"maunium.net/go/mautrix/crypto/backup"
	"maunium.net/go/mautrix/crypto/ssss"
	"maunium.net/go/mautrix/event"
)

// RestoreKeyBackup restores Megolm session keys from the homeserver's key
// backup using the SSSS recovery key.
//
// When recoveryKey is empty the restore is skipped. Any error encountered at
// any step is logged and the function returns without aborting startup - key
// backup restore is best-effort.
func (m *CryptoManager) RestoreKeyBackup(ctx context.Context, recoveryKey string) {
	if recoveryKey == "" {
		return
	}

	// # Resolve SSSS key
	keyID, keyData, err := m.mach.SSSS.GetDefaultKeyData(ctx)
	if err != nil {
		slog.WarnContext(ctx, "key backup: failed to get SSSS key data",
			"account_id", m.accountID,
			"error", err,
		)
		return
	}

	ssssKey, err := keyData.VerifyRecoveryKey(keyID, recoveryKey)
	if errors.Is(err, ssss.ErrUnverifiableKey) {
		slog.WarnContext(ctx, "key backup: SSSS key unverifiable, proceeding anyway",
			"account_id", m.accountID,
			"key_id", keyID,
		)
	} else if err != nil {
		slog.WarnContext(ctx, "key backup: recovery key verification failed",
			"account_id", m.accountID,
			"error", err,
		)
		return
	}

	// # Fetch and decrypt the Megolm backup private key from account data
	rawKey, err := m.mach.SSSS.GetDecryptedAccountData(ctx, event.AccountDataMegolmBackupKey, ssssKey)
	if err != nil {
		slog.WarnContext(ctx, "key backup: failed to decrypt megolm backup key from account data",
			"account_id", m.accountID,
			"error", err,
		)
		return
	}

	megolmBackupKey, err := backup.MegolmBackupKeyFromBytes(rawKey)
	if err != nil {
		slog.WarnContext(ctx, "key backup: invalid megolm backup key bytes",
			"account_id", m.accountID,
			"error", err,
		)
		return
	}

	// # Download and import all backed-up Megolm sessions
	version, err := m.mach.DownloadAndStoreLatestKeyBackup(ctx, megolmBackupKey)
	if err != nil {
		slog.WarnContext(ctx, "key backup: download and store failed",
			"account_id", m.accountID,
			"error", err,
		)
		return
	}

	slog.InfoContext(ctx, "key backup: restore complete",
		"account_id", m.accountID,
		"backup_version", version,
	)
}
