package account

import (
	"context"
	"fmt"

	"maunium.net/go/mautrix/id"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/config"
	"github.com/ilamparithi-in/matfix/internal/crypto"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/media"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	syncsvc "github.com/ilamparithi-in/matfix/internal/sync"
)

// startAccount creates and starts the full per-account component tree:
//
//  1. Engine client — wraps the mautrix.Client for this account.
//  2. Crypto manager — initialises OlmMachine, upgrades schemas, restores key backup.
//  3. Sync manager — starts the /sync loop using the crypto manager as a Decrypter.
//
// On any error the whole account is considered failed and the error is returned.
// The caller is responsible for recording the failure in an AccountContext.
func startAccount(
	ctx context.Context,
	acfg config.AccountConfig,
	cryptoCfg config.CryptoConfig,
	db *persistence.DB,
	b bus.Bus,
) (*AccountContext, error) {
	// # Engine client
	client, err := engine.NewClient(engine.ClientConfig{
		AccountID:     acfg.ID,
		HomeserverURL: acfg.HomeserverURL,
		UserID:        id.UserID(acfg.UserID),
		AccessToken:   acfg.AccessToken,
		DeviceID:      id.DeviceID(acfg.DeviceID),
	})
	if err != nil {
		return nil, fmt.Errorf("account %s: init client: %w", acfg.ID, err)
	}

	// # Media manager
	client.SetMediaManager(media.NewManager(client.Underlying()))

	// # Crypto manager
	cm, err := crypto.New(ctx, crypto.Config{
		AccountCfg: acfg,
		CryptoCfg:  cryptoCfg,
		Client:     client,
		DB:         db,
		Bus:        b,
	})
	if err != nil {
		return nil, fmt.Errorf("account %s: init crypto: %w", acfg.ID, err)
	}
	cm.Start()

	// # Sync manager
	stores := db.Stores()
	sm := syncsvc.New(syncsvc.Config{
		AccountID:  acfg.ID,
		Client:     client,
		SyncStore:  stores.Sync,
		CacheStore: stores.EventCache,
		Bus:        b,
		Decrypter:  cm,
	})
	sm.Start(ctx)

	return &AccountContext{
		accountID: acfg.ID,
		client:    client,
		sync:      sm,
		crypto:    cm,
		status:    StatusAvailable,
	}, nil
}

// stopAccount gracefully stops an account's sync and crypto components.
// It is safe to call on a failed AccountContext (nil component pointers are
// checked before stopping).
func stopAccount(actx *AccountContext) {
	if actx.sync != nil {
		actx.sync.Stop()
	}
	if actx.crypto != nil {
		actx.crypto.Stop()
	}
}
