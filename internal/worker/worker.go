package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/queue"
)

// pollInterval is the fallback wake interval when the notify channel is quiet.
const pollInterval = 2 * time.Second

// runWorker is the body of a single worker goroutine. It cycles through all
// configured accounts, pulling and processing one job per account per pass.
// When all accounts are idle it blocks until a notify signal or the poll ticker.
//
// Panics are recovered so the goroutine restarts its loop rather than crashing
// the whole process.
func runWorker(ctx context.Context, cfg Config) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		// Attempt one delivery per account; track whether any work was done.
		worked := false
		for _, accountID := range cfg.Accounts {
			if ctx.Err() != nil {
				return
			}
			if processOne(ctx, cfg, accountID) {
				worked = true
			}
		}

		if worked {
			// There may be more jobs; try again immediately.
			continue
		}

		// All accounts idle - wait for a signal or fallback tick.
		select {
		case <-ctx.Done():
			return
		case <-cfg.Manager.Notify():
			// A new job was enqueued; loop immediately.
		case <-ticker.C:
			// Fallback poll interval elapsed.
		}
	}
}

// processOne attempts to pull and deliver one job for accountID.
// Returns true if a job was processed (regardless of success or failure),
// false if no eligible job was available.
func processOne(ctx context.Context, cfg Config, accountID string) (processed bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("worker: panic recovered",
				"account_id", accountID,
				"panic", r,
			)
			processed = true // avoid tight-loop on repeated panics
		}
	}()

	job, err := cfg.Manager.PullNext(ctx, accountID)
	if err != nil {
		slog.Error("worker: pull next job failed",
			"account_id", accountID,
			"error", err,
		)
		return false
	}
	if job == nil {
		return false
	}

	client, ok := cfg.Clients(accountID)
	if !ok {
		// Account unavailable; put the job back so it can be tried later.
		slog.Warn("worker: engine client unavailable, rescheduling job",
			"account_id", accountID,
			"job_id", job.ID,
		)
		if _, _, retryErr := cfg.Manager.ScheduleRetry(ctx, job.ID, job.RetryCount); retryErr != nil {
			slog.Error("worker: failed to reschedule job after missing client",
				"job_id", job.ID,
				"error", retryErr,
			)
		}
		return true
	}

	matrixEventID, sendErr := client.Send(ctx, job.RoomID, job.Request)
	if sendErr != nil {
		onSendFailure(ctx, cfg, job, accountID, sendErr.Error())
		return true
	}

	// Delivery succeeded.
	if err := cfg.Manager.Acknowledge(ctx, job.ID, string(matrixEventID)); err != nil {
		slog.Error("worker: failed to mark job acknowledged",
			"job_id", job.ID,
			"error", err,
		)
	}

	cfg.Bus.Publish(bus.EventEnvelope{
		Type:      bus.EventDeliveryAcknowledged,
		AccountID: accountID,
		RoomID:    string(job.RoomID),
		Payload: bus.DeliveryAcknowledgedEvent{
			JobID:         job.ID,
			MatrixEventID: string(matrixEventID),
			Timestamp:     time.Now(),
		},
	})
	return true
}

// onSendFailure schedules a retry or dead-letters the job and publishes the
// appropriate bus event.
func onSendFailure(ctx context.Context, cfg Config, job *queue.Job, accountID, errMsg string) {
	slog.Warn("worker: send failed",
		"account_id", accountID,
		"job_id", job.ID,
		"retry_count", job.RetryCount,
		"error", errMsg,
	)

	retried, scheduledAt, retryErr := cfg.Manager.ScheduleRetry(ctx, job.ID, job.RetryCount)
	if retryErr != nil {
		slog.Error("worker: failed to schedule retry",
			"job_id", job.ID,
			"error", retryErr,
		)
		return
	}

	if retried {
		cfg.Bus.Publish(bus.EventEnvelope{
			Type:      bus.EventRetryScheduled,
			AccountID: accountID,
			RoomID:    string(job.RoomID),
			Payload: bus.RetryScheduledEvent{
				JobID:       job.ID,
				RetryCount:  job.RetryCount + 1,
				ScheduledAt: scheduledAt,
			},
		})
	} else {
		cfg.Bus.Publish(bus.EventEnvelope{
			Type:      bus.EventRetryExhausted,
			AccountID: accountID,
			RoomID:    string(job.RoomID),
			Payload: bus.RetryExhaustedEvent{
				JobID:      job.ID,
				RetryCount: job.RetryCount,
				LastError:  errMsg,
			},
		})
	}
}
