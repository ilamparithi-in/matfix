package sync

import (
	"context"
	"log/slog"
	"time"

	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # Deduplication

// isSeen returns true if eventID is new (not yet in the event cache) and
// should be dispatched. Returns true on cache errors to avoid dropping events.
//
// Callers must invoke markSeen after publishing to complete the record.
func (m *SyncManager) isSeen(ctx context.Context, eventID string) bool {
	if eventID == "" {
		// Events without IDs (e.g. some state events) are always treated as new.
		return true
	}
	has, err := m.cacheStore.Has(ctx, eventID, m.accountID)
	if err != nil {
		slog.Warn("sync: event cache lookup failed; allowing event through",
			"account_id", m.accountID,
			"event_id", eventID,
			"error", err,
		)
		return true
	}
	return !has
}

// markSeen records eventID in the event cache for this account.
// Must be called after the event has been published to the bus.
// Errors are logged and ignored; a missing record causes at-most-one
// extra delivery on restart (acceptable under best-effort semantics).
func (m *SyncManager) markSeen(ctx context.Context, eventID string) {
	if eventID == "" {
		return
	}
	if err := m.cacheStore.Insert(ctx, persistence.EventCacheEntry{
		EventID:   eventID,
		AccountID: m.accountID,
		SeenAt:    time.Now().UnixMilli(),
	}); err != nil {
		slog.Warn("sync: failed to record event in cache",
			"account_id", m.accountID,
			"event_id", eventID,
			"error", err,
		)
	}
}
