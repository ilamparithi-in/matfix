package correlation

import (
	"context"
	"log/slog"
	"time"
)

// cleanupInterval is how often the cleanup goroutine sweeps for expired entries.
const cleanupInterval = 10 * time.Second

// runCleanup periodically sweeps expired in-memory handles and prunes stale DB
// entries. It runs until ctx is cancelled.
func (m *CorrelationManager) runCleanup(ctx context.Context) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepExpired(ctx)
		}
	}
}

// sweepExpired expires any in-memory handles that have passed their deadline and
// removes resolved/expired rows from the database.
//
// The per-handle timeout goroutines handle expiry responsively; this sweep is a
// belt-and-suspenders pass to catch any handles that slipped through and to keep
// the DB tidy.
func (m *CorrelationManager) sweepExpired(ctx context.Context) {
	now := time.Now()

	m.mu.RLock()
	var expiredAsks []*activeAsk
	for _, a := range m.asks {
		if now.After(a.timeoutAt) {
			expiredAsks = append(expiredAsks, a)
		}
	}
	var expiredReceives []*activeReceive
	for _, r := range m.receives {
		if now.After(r.timeoutAt) {
			expiredReceives = append(expiredReceives, r)
		}
	}
	m.mu.RUnlock()

	for _, a := range expiredAsks {
		m.resolveAskTimeout(a)
	}
	for _, r := range expiredReceives {
		m.resolveReceive(r, "expired")
	}

	// Prune rows already in a terminal state from the database.
	n, err := m.store.DeleteExpired(ctx, now.UnixMilli())
	if err != nil {
		slog.Error("correlation: cleanup sweep failed", "error", err)
		return
	}
	if n > 0 {
		slog.Debug("correlation: pruned expired entries", "count", n)
	}
}
