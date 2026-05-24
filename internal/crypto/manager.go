package crypto

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	mcrypto "maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/sql_store_upgrade"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/sqlstatestore"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/config"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # Types

// Config holds construction parameters for a CryptoManager.
type Config struct {
	AccountCfg config.AccountConfig
	CryptoCfg  config.CryptoConfig
	Client     *engine.Client
	DB         *persistence.DB
	Bus        bus.Bus
}

// CryptoManager owns the OlmMachine lifecycle for a single Matrix account.
type CryptoManager struct {
	accountID  string
	mach       *mcrypto.OlmMachine
	stateStore *sqlstatestore.SQLStateStore
	bus        bus.Bus
	cancel     context.CancelFunc
}

// # Constructor

// New creates and initialises a CryptoManager for cfg.AccountCfg.
// It upgrades the crypto and state-store schemas, loads the Olm account,
// applies the trust policy, and optionally restores the Megolm key backup.
func New(ctx context.Context, cfg Config) (*CryptoManager, error) {
	// # Database setup
	db, err := dbutil.NewWithDB(cfg.DB.SQL(), "sqlite")
	if err != nil {
		return nil, fmt.Errorf("crypto: wrap dbutil: %w", err)
	}

	cryptoDB := db.Child("crypto_version", sql_store_upgrade.Table, nil)
	if err := cryptoDB.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("crypto: upgrade crypto schema: %w", err)
	}

	stateStore := sqlstatestore.NewSQLStateStore(db, nil, false)
	if err := stateStore.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("crypto: upgrade state store schema: %w", err)
	}

	// # Crypto store - isolated per account + device pair.
	pickleKey := derivePickleKey(cfg.AccountCfg)
	cryptoStore := mcrypto.NewSQLCryptoStore(
		cryptoDB, nil,
		cfg.AccountCfg.ID,
		id.DeviceID(cfg.AccountCfg.DeviceID),
		pickleKey,
	)

	// # OlmMachine
	noop := zerolog.Nop()
	mach := mcrypto.NewOlmMachine(cfg.Client.Underlying(), &noop, cryptoStore, stateStore)
	applyTrustPolicy(mach, cfg.CryptoCfg.TrustPolicy)

	if err := mach.Load(ctx); err != nil {
		return nil, fmt.Errorf("crypto: load olm account: %w", err)
	}

	cm := &CryptoManager{
		accountID:  cfg.AccountCfg.ID,
		mach:       mach,
		stateStore: stateStore,
		bus:        cfg.Bus,
	}

	// Restore Megolm key backup on startup; errors are logged, not fatal.
	cm.RestoreKeyBackup(ctx, cfg.AccountCfg.RecoveryKey)

	return cm, nil
}

// # Lifecycle

// Start arms the CryptoManager's background cancel function.
func (m *CryptoManager) Start() {
	_, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
}

// Stop cancels background operations.
func (m *CryptoManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// # Sync integration

// ProcessSyncResponse feeds incoming to-device events and device-list changes
// from a sync response into the OlmMachine so Olm sessions remain current.
// Must be called before decrypting events from the same response.
func (m *CryptoManager) ProcessSyncResponse(ctx context.Context, resp *mautrix.RespSync, since string) {
	m.mach.ProcessSyncResponse(ctx, resp, since)
}

// # Helpers

// derivePickleKey derives a deterministic 32-byte key for the OLM account
// pickle from the account's static credentials.
func derivePickleKey(cfg config.AccountConfig) []byte {
	h := sha256.New()
	h.Write([]byte(cfg.ID))
	h.Write([]byte(":"))
	h.Write([]byte(cfg.DeviceID))
	h.Write([]byte(":"))
	h.Write([]byte(cfg.AccessToken))
	return h.Sum(nil)
}
