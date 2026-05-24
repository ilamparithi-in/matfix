package account

import (
	"context"
	gosync "sync"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/config"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # AccountManager

// AccountManager owns the lifecycle of all configured Matrix accounts.
// It initialises per-account component trees, tracks availability, and provides
// controlled access to individual accounts.
type AccountManager struct {
	mu       gosync.RWMutex
	accounts map[string]*AccountContext
	cfg      config.Config
	db       *persistence.DB
	bus      bus.Bus
}

// New creates an AccountManager. Call StartAll to initialise the accounts.
func New(cfg config.Config, db *persistence.DB, b bus.Bus) *AccountManager {
	return &AccountManager{
		accounts: make(map[string]*AccountContext),
		cfg:      cfg,
		db:       db,
		bus:      b,
	}
}

// StartAll initialises all configured accounts and starts their component trees.
// Failed accounts are recorded individually; the relay continues with any
// accounts that started successfully.
//
// Returns ErrAllAccountsFailed when every account fails to start; the caller
// should treat this as a fatal condition and terminate the process.
func (m *AccountManager) StartAll(ctx context.Context) error {
	for _, acfg := range m.cfg.Accounts {
		actx, err := startAccount(ctx, acfg, m.cfg.Crypto, m.db, m.bus)
		if err != nil {
			actx = &AccountContext{
				accountID: acfg.ID,
				status:    StatusFailed,
				err:       err,
			}
		}
		m.mu.Lock()
		m.accounts[acfg.ID] = actx
		m.mu.Unlock()
	}
	return checkFailures(m.accounts)
}

// StopAll gracefully stops every running account's component tree.
func (m *AccountManager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, actx := range m.accounts {
		stopAccount(actx)
	}
}

// Get returns the AccountContext for accountID, or nil if not found.
func (m *AccountManager) Get(accountID string) *AccountContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accounts[accountID]
}

// Available returns all AccountContexts in StatusAvailable state.
func (m *AccountManager) Available() []*AccountContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*AccountContext
	for _, actx := range m.accounts {
		if actx.IsAvailable() {
			out = append(out, actx)
		}
	}
	return out
}

// All returns all AccountContexts regardless of status.
func (m *AccountManager) All() []*AccountContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*AccountContext, 0, len(m.accounts))
	for _, actx := range m.accounts {
		out = append(out, actx)
	}
	return out
}
