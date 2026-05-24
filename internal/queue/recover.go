package queue

import (
	"context"
	"fmt"
	"log/slog"
)

// RecoverOnStartup moves all jobs stuck in state "sending" back to "queued".
// This corrects state left behind by a crash or abrupt shutdown before the
// worker pool is started.
func (m *QueueManager) RecoverOnStartup(ctx context.Context) error {
	n, err := m.store.RestoreStuck(ctx)
	if err != nil {
		return fmt.Errorf("queue: recover on startup: %w", err)
	}
	if n > 0 {
		slog.Warn("queue: restored stuck jobs to queued on startup", "count", n)
	}
	return nil
}
