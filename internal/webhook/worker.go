package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// RuleResolver looks up active webhook rules by ID.
type RuleResolver func(id string) *WebhookRule

// RetryWorker pulls failed webhook delivery jobs from SQLite and executes retries.
type RetryWorker struct {
	store       persistence.WebhookStore
	dispatcher  *Dispatcher
	ruleLookup  RuleResolver
	concurrency int
	wakeCh      chan struct{}
	stopCh      chan struct{}
	loopWg      sync.WaitGroup
	stopOnce    sync.Once
}

// NewRetryWorker creates a RetryWorker.
func NewRetryWorker(store persistence.WebhookStore, dispatcher *Dispatcher, concurrency int, ruleLookup RuleResolver) *RetryWorker {
	if concurrency <= 0 {
		concurrency = 4
	}
	return &RetryWorker{
		store:       store,
		dispatcher:  dispatcher,
		ruleLookup:  ruleLookup,
		concurrency: concurrency,
		wakeCh:      make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
}

// SignalWake signals the worker to check for pending retries immediately.
func (w *RetryWorker) SignalWake() {
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

// Start launches the retry loop in a background goroutine.
func (w *RetryWorker) Start(ctx context.Context) {
	w.loopWg.Add(1)
	go func() {
		defer w.loopWg.Done()
		w.run(ctx)
	}()
}

// run runs the retry loop until ctx is canceled or Stop() is called.
func (w *RetryWorker) run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processPending(ctx)
		case <-w.wakeCh:
			w.processPending(ctx)
		}
	}
}

// Stop cleanly terminates the retry worker and waits for active retries to drain.
func (w *RetryWorker) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
	w.loopWg.Wait()
}

func (w *RetryWorker) processPending(ctx context.Context) {
	if w.store == nil {
		return
	}

	batchSize := w.concurrency * 2
	for {
		if ctx.Err() != nil {
			return
		}

		now := time.Now().UnixMilli()
		entries, err := w.store.AcquireNextRetry(ctx, now, batchSize)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("webhook retry worker: acquire failed", "error", err)
			}
			return
		}
		if len(entries) == 0 {
			return
		}

		var batchWg sync.WaitGroup
		sem := make(chan struct{}, w.concurrency)

		for _, entry := range entries {
			if ctx.Err() != nil {
				break
			}
			batchWg.Add(1)
			sem <- struct{}{}
			go func(e persistence.WebhookQueueEntry) {
				defer batchWg.Done()
				defer func() { <-sem }()
				w.processEntry(ctx, e)
			}(entry)
		}
		batchWg.Wait()

		// If we pulled fewer entries than the batch size, all due retries have been processed
		if len(entries) < batchSize {
			return
		}
	}
}

func (w *RetryWorker) processEntry(ctx context.Context, entry persistence.WebhookQueueEntry) {
	var headers map[string]string
	if entry.HeadersJSON != "" {
		_ = json.Unmarshal([]byte(entry.HeadersJSON), &headers)
	}

	res := w.dispatcher.Dispatch(ctx, DispatchParams{
		DeliveryID: entry.ID,
		WebhookID:  entry.WebhookID,
		URL:        entry.URL,
		Secret:     entry.Secret,
		Headers:    headers,
		Payload:    []byte(entry.Payload),
		Timeout:    10 * time.Second,
	})

	if res.Success {
		if err := w.store.DeleteSuccess(ctx, entry.ID); err != nil {
			slog.Error("webhook retry: delete success failed", "job_id", entry.ID, "error", err)
		}
		return
	}

	// If 4xx client error, dead-letter immediately to prevent looping on bad URLs/auth
	if res.StatusCode >= 400 && res.StatusCode < 500 {
		errMsg := "client error: " + res.Err.Error()
		if err := w.store.MarkDeadLetter(ctx, entry.ID, res.StatusCode, errMsg); err != nil {
			slog.Error("webhook retry: mark dead letter failed", "job_id", entry.ID, "error", err)
		}
		return
	}

	// Check if max retries exceeded
	if entry.RetryCount >= entry.MaxRetries {
		errMsg := "max retries exhausted: " + res.Err.Error()
		if err := w.store.MarkDeadLetter(ctx, entry.ID, res.StatusCode, errMsg); err != nil {
			slog.Error("webhook retry: mark dead letter failed", "job_id", entry.ID, "error", err)
		}
		return
	}

	// Dynamic backoff interval from rule if available
	initialInterval := 5 * time.Second
	maxInterval := 1 * time.Hour
	if w.ruleLookup != nil {
		if rule := w.ruleLookup(entry.WebhookID); rule != nil {
			initialInterval = rule.InitialIntervalDuration()
			maxInterval = rule.MaxIntervalDuration()
		}
	}

	backoff := computeBackoff(entry.RetryCount, initialInterval, maxInterval)
	nextSched := time.Now().Add(backoff).UnixMilli()

	if err := w.store.UpdateRetryFailure(ctx, entry.ID, nextSched, res.StatusCode, res.Err.Error()); err != nil {
		slog.Error("webhook retry: update failure failed", "job_id", entry.ID, "error", err)
	}
}

// computeBackoff calculates exponential backoff with full jitter:
// interval = min(maxInterval, initialInterval * 2^attempt)
// backoff = interval/2 + rand(0, interval/2)
func computeBackoff(attempt int, initial, max time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 30 {
		attempt = 30
	}
	multiplier := 1 << attempt
	dur := initial * time.Duration(multiplier)
	if dur > max || dur <= 0 {
		dur = max
	}
	half := dur / 2
	if half <= 0 {
		return dur
	}
	jitter := time.Duration(rand.Int63n(int64(half)))
	return half + jitter
}
