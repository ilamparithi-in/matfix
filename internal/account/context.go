package account

import (
	"github.com/ilamparithi-in/matfix/internal/crypto"
	"github.com/ilamparithi-in/matfix/internal/engine"
	syncsvc "github.com/ilamparithi-in/matfix/internal/sync"
)

// # AccountStatus

// AccountStatus represents the operational state of a single Matrix account.
type AccountStatus int

const (
	// StatusInitializing means the account's component tree is being built.
	StatusInitializing AccountStatus = iota
	// StatusAvailable means all components started successfully and the account
	// is ready to send and receive messages.
	StatusAvailable
	// StatusFailed means at least one required component failed to start.
	// The account cannot be used until restarted.
	StatusFailed
)

// # AccountContext

// AccountContext groups all per-account components with the account's
// availability state. A single AccountContext is owned by AccountManager.
type AccountContext struct {
	accountID string
	client    *engine.Client
	sync      *syncsvc.SyncManager
	crypto    *crypto.CryptoManager
	status    AccountStatus
	err       error // non-nil when status == StatusFailed
}

// AccountID returns the opaque account identifier from the daemon configuration.
func (a *AccountContext) AccountID() string { return a.accountID }

// IsAvailable reports whether this account is fully operational.
func (a *AccountContext) IsAvailable() bool { return a.status == StatusAvailable }

// Client returns the engine client for this account.
func (a *AccountContext) Client() *engine.Client { return a.client }

// SyncManager returns the sync manager for this account.
func (a *AccountContext) SyncManager() *syncsvc.SyncManager { return a.sync }

// CryptoManager returns the crypto manager for this account.
// May be nil when the account is in StatusFailed state.
func (a *AccountContext) CryptoManager() *crypto.CryptoManager { return a.crypto }

// Err returns the error that caused this account to enter StatusFailed, or nil.
func (a *AccountContext) Err() error { return a.err }
