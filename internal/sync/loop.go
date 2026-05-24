package sync

import (
	"context"
	"log/slog"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
)

// syncLongPollMS is the /sync long-poll timeout once the client is caught up.
const syncLongPollMS = 30_000

// run is the /sync loop body. It blocks until ctx is cancelled.
//
// Token advance rule: the next_batch token is persisted to the DB only after
// all events in a batch have been published to the bus. This preserves replay
// guarantees across restarts.
func (m *SyncManager) run(ctx context.Context) {
	since, err := m.syncStore.GetNextBatch(ctx, m.accountID)
	if err != nil {
		slog.Error("sync: failed to load next_batch; starting from empty token",
			"account_id", m.accountID,
			"error", err,
		)
		since = ""
	}

	mx := m.client.Underlying()
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		// Use timeout=0 on first sync (no token) and after any failure so we
		// get an immediate response rather than blocking the long-poll.
		timeout := syncLongPollMS
		if since == "" {
			timeout = 0
		}

		resp, err := mx.FullSyncRequest(ctx, mautrix.ReqSync{
			Timeout:     timeout,
			Since:       since,
			SetPresence: event.PresenceOffline,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("sync: /sync request failed; will retry",
				"account_id", m.accountID,
				"error", err,
				"backoff", backoff,
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = clampDuration(backoff*2, 60*time.Second)
			continue
		}
		backoff = time.Second // reset on any successful response

		// Publish all events from this batch.
		// Context cancellation is checked inside processResponse; if cancelled
		// mid-batch we return here without advancing the token so the batch
		// replays from the same since value on restart.
		m.processResponse(ctx, resp)
		if ctx.Err() != nil {
			return
		}

		// Advance the token only after a successful publish pass.
		since = resp.NextBatch
		if err := m.syncStore.SetNextBatch(ctx, m.accountID, since); err != nil {
			slog.Error("sync: failed to persist next_batch",
				"account_id", m.accountID,
				"error", err,
			)
			// Continue: worst-case we re-process events on restart;
			// the event dedup cache prevents double-publishing.
		}
	}
}

// processResponse dispatches every event in resp to the bus.
func (m *SyncManager) processResponse(ctx context.Context, resp *mautrix.RespSync) {
	for roomID, roomData := range resp.Rooms.Join {
		if ctx.Err() != nil {
			return
		}
		rid := string(roomID)
		for _, evt := range roomData.State.Events {
			m.dispatchEvent(ctx, rid, evt)
		}
		for _, evt := range roomData.Timeline.Events {
			m.dispatchEvent(ctx, rid, evt)
		}
		for _, evt := range roomData.Ephemeral.Events {
			m.dispatchEphemeral(ctx, rid, evt)
		}
	}

	for roomID, roomData := range resp.Rooms.Leave {
		if ctx.Err() != nil {
			return
		}
		rid := string(roomID)
		for _, evt := range roomData.State.Events {
			m.dispatchEvent(ctx, rid, evt)
		}
		for _, evt := range roomData.Timeline.Events {
			m.dispatchEvent(ctx, rid, evt)
		}
	}
}

// clampDuration returns d capped at max.
func clampDuration(d, max time.Duration) time.Duration {
	if d > max {
		return max
	}
	return d
}
