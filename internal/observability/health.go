package observability

import (
	"context"
	"time"

	"github.com/ilamparithi-in/matfix/internal/account"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # HealthChecker

// HealthChecker evaluates the liveness and readiness of the daemon.
//
// Liveness: the process is running (always true once the checker exists).
//
// Readiness: the SQLite database is reachable AND at least one Matrix account
// is in the available state.
type HealthChecker struct {
	db       *persistence.DB
	accounts *account.AccountManager
}

// NewHealthChecker creates a HealthChecker using the supplied DB and account manager.
func NewHealthChecker(db *persistence.DB, accounts *account.AccountManager) *HealthChecker {
	return &HealthChecker{db: db, accounts: accounts}
}

// IsLive always returns true — the process is alive if it can call this method.
func (h *HealthChecker) IsLive() bool { return true }

// IsReady returns true when the DB is reachable and at least one account is
// available.
func (h *HealthChecker) IsReady() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.db.SQL().PingContext(ctx); err != nil {
		return false
	}
	return len(h.accounts.Available()) > 0
}

// ReadinessDetail returns a structured snapshot of the current readiness state.
// It is used by the /health/ready HTTP handler to populate response bodies.
type ReadinessDetail struct {
	// Ready is true when the daemon can accept API requests.
	Ready bool
	// DBReachable indicates whether the SQLite ping succeeded.
	DBReachable bool
	// Accounts maps each account ID to its current status string.
	Accounts map[string]string
}

// Detail returns a ReadinessDetail snapshot.
func (h *HealthChecker) Detail() ReadinessDetail {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dbOK := h.db.SQL().PingContext(ctx) == nil

	all := h.accounts.All()
	accountMap := make(map[string]string, len(all))
	for _, actx := range all {
		if actx.IsAvailable() {
			accountMap[actx.AccountID()] = "available"
		} else {
			accountMap[actx.AccountID()] = "failed"
		}
	}

	available := h.accounts.Available()
	ready := dbOK && len(available) > 0

	return ReadinessDetail{
		Ready:       ready,
		DBReachable: dbOK,
		Accounts:    accountMap,
	}
}
