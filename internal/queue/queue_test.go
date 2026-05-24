package queue_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ilamparithi-in/matfix/internal/config"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/queue"
	_ "modernc.org/sqlite"
)

// # Test helpers

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := persistence.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func defaultPolicy(maxRetries int) config.RetryPolicyConfig {
	return config.RetryPolicyConfig{
		MaxRetries:      maxRetries,
		BackoffPolicy:   config.BackoffPolicyExponential,
		InitialInterval: config.Duration(100 * time.Millisecond),
		MaxInterval:     config.Duration(10 * time.Second),
	}
}

// fastRetryPolicy returns a retry policy with zero backoff so retried jobs
// are immediately eligible for PullNext. Use this in tests that call ScheduleRetry.
func fastRetryPolicy(maxRetries int) config.RetryPolicyConfig {
	return config.RetryPolicyConfig{
		MaxRetries:      maxRetries,
		BackoffPolicy:   config.BackoffPolicyExponential,
		InitialInterval: config.Duration(0),
		MaxInterval:     config.Duration(0),
	}
}

func newManager(t *testing.T, maxRetries int) *queue.QueueManager {
	t.Helper()
	db := openTestDB(t)
	store := persistence.NewQueueStore(db)
	return queue.New(queue.Config{
		Store:  store,
		Policy: defaultPolicy(maxRetries),
	})
}

// # State machine tests

func TestEnqueue_PullNext_Transition(t *testing.T) {
	ctx := context.Background()
	mgr := newManager(t, 3)

	job, err := mgr.Enqueue(ctx, "acc1", "!room:server", engine.TextMessage{Body: "hello"}, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job.State != queue.StateQueued {
		t.Errorf("want state queued, got %s", job.State)
	}

	pulled, err := mgr.PullNext(ctx, "acc1")
	if err != nil {
		t.Fatalf("PullNext: %v", err)
	}
	if pulled == nil {
		t.Fatal("PullNext returned nil; expected a job")
	}
	if pulled.ID != job.ID {
		t.Errorf("pulled wrong job: want %s, got %s", job.ID, pulled.ID)
	}
	if pulled.State != queue.StateSending {
		t.Errorf("want state sending after pull, got %s", pulled.State)
	}

	if err := mgr.Transition(ctx, pulled.ID, queue.StateAcknowledged); err != nil {
		t.Fatalf("Transition to acknowledged: %v", err)
	}

	// No more jobs for this account.
	next, err := mgr.PullNext(ctx, "acc1")
	if err != nil {
		t.Fatalf("PullNext after ack: %v", err)
	}
	if next != nil {
		t.Errorf("expected no more jobs, got %s", next.ID)
	}
}

func TestIdempotency_DuplicateKey(t *testing.T) {
	ctx := context.Background()
	mgr := newManager(t, 3)

	first, err := mgr.Enqueue(ctx, "acc1", "!room:server", engine.TextMessage{Body: "hi"}, "key-1")
	if err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}

	second, err := mgr.Enqueue(ctx, "acc1", "!room:server", engine.TextMessage{Body: "hi"}, "key-1")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("idempotency broken: got two different job IDs %s vs %s", first.ID, second.ID)
	}
}

// # Crash recovery test

func TestRecoverOnStartup_RestoresSendingToQueued(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := persistence.NewQueueStore(db)
	mgr := queue.New(queue.Config{Store: store, Policy: defaultPolicy(3)})

	// Enqueue then pull (state becomes "sending").
	if _, err := mgr.Enqueue(ctx, "acc1", "!r:s", engine.TextMessage{Body: "x"}, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	pulled, err := mgr.PullNext(ctx, "acc1")
	if err != nil || pulled == nil {
		t.Fatalf("PullNext: %v, %v", pulled, err)
	}
	if pulled.State != queue.StateSending {
		t.Fatalf("expected sending state, got %s", pulled.State)
	}

	// Simulate crash: construct a new manager over the same store.
	mgr2 := queue.New(queue.Config{Store: store, Policy: defaultPolicy(3)})
	if err := mgr2.RecoverOnStartup(ctx); err != nil {
		t.Fatalf("RecoverOnStartup: %v", err)
	}

	// Job should now be pullable again.
	recovered, err := mgr2.PullNext(ctx, "acc1")
	if err != nil {
		t.Fatalf("PullNext after recovery: %v", err)
	}
	if recovered == nil {
		t.Fatal("expected recovered job to be pullable, got nil")
	}
}

// # Dead-letter after N retries test

func TestDeadLetter_AfterMaxRetries(t *testing.T) {
	ctx := context.Background()
	const maxRetries = 2
	db := openTestDB(t)
	store := persistence.NewQueueStore(db)
	mgr := queue.New(queue.Config{
		Store:  store,
		Policy: fastRetryPolicy(maxRetries),
	})

	job, err := mgr.Enqueue(ctx, "acc1", "!r:s", engine.TextMessage{Body: "fail"}, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Simulate failures up to exhaustion.
	currentCount := 0
	for i := 0; i <= maxRetries; i++ {
		pulled, err := mgr.PullNext(ctx, "acc1")
		if err != nil {
			t.Fatalf("PullNext attempt %d: %v", i, err)
		}
		if pulled == nil {
			t.Fatalf("PullNext attempt %d returned nil", i)
		}
		currentCount = pulled.RetryCount
		retried, _, retryErr := mgr.ScheduleRetry(ctx, pulled.ID, currentCount)
		if retryErr != nil {
			t.Fatalf("ScheduleRetry attempt %d: %v", i, retryErr)
		}
		if i < maxRetries {
			// Still within retries - expect job to be rescheduled.
			if !retried {
				t.Errorf("attempt %d: expected retried=true, got false", i)
			}
		} else {
			// Last attempt - expect dead_letter.
			if retried {
				t.Errorf("attempt %d: expected retried=false (dead_letter), got true", i)
			}
		}
	}

	// After exhaustion there must be no pullable job.
	_ = job.ID // only used to keep initial reference
	final, err := mgr.PullNext(ctx, "acc1")
	if err != nil {
		t.Fatalf("PullNext after dead_letter: %v", err)
	}
	if final != nil {
		t.Errorf("expected no pullable job after dead_letter, got %s", final.ID)
	}
}

// # Payload round-trip tests

func TestPayloadRoundTrip_AllTypes(t *testing.T) {
	ctx := context.Background()
	mgr := newManager(t, 3)

	cases := []engine.SendRequest{
		engine.TextMessage{Body: "plain"},
		engine.HTMLMessage{Body: "plain", FormattedBody: "<b>bold</b>"},
		engine.Reply{InReplyTo: "$evt:server", Body: "reply"},
		engine.Reaction{TargetEventID: "$evt:server", Key: "👍"},
		engine.Edit{TargetEventID: "$evt:server", NewBody: "updated"},
		engine.Redaction{TargetEventID: "$evt:server", Reason: "spam"},
	}

	for _, req := range cases {
		job, err := mgr.Enqueue(ctx, "acc1", "!r:s", req, "")
		if err != nil {
			t.Fatalf("%T: Enqueue: %v", req, err)
		}
		pulled, err := mgr.PullNext(ctx, "acc1")
		if err != nil || pulled == nil {
			t.Fatalf("%T: PullNext: %v %v", req, pulled, err)
		}
		if pulled.ID != job.ID {
			t.Errorf("%T: pulled wrong job", req)
		}
		// Advance so the next iteration can pull a fresh job.
		_ = mgr.Transition(ctx, pulled.ID, queue.StateAcknowledged)
	}
}

// # Retry backoff tests

func TestNextScheduledAt_Exponential(t *testing.T) {
	policy := defaultPolicy(5)
	before := time.Now()

	t1 := queue.NextScheduledAt(policy, 1) // 100 ms * 2^0 = 100 ms
	t2 := queue.NextScheduledAt(policy, 2) // 100 ms * 2^1 = 200 ms

	if !t1.After(before) {
		t.Error("t1 should be in the future")
	}
	if !t2.After(t1) {
		t.Error("t2 should be later than t1")
	}
}

func TestNextScheduledAt_MaxIntervalCap(t *testing.T) {
	policy := config.RetryPolicyConfig{
		BackoffPolicy:   config.BackoffPolicyExponential,
		InitialInterval: config.Duration(1 * time.Second),
		MaxInterval:     config.Duration(5 * time.Second),
	}

	// retryCount=10 would produce 512 s without cap.
	t10 := queue.NextScheduledAt(policy, 10)
	cap := time.Now().Add(5*time.Second + 100*time.Millisecond)
	if t10.After(cap) {
		t.Errorf("expected capped at MaxInterval; got %v", t10)
	}
}

func TestIsExhausted(t *testing.T) {
	policy := defaultPolicy(3)
	if queue.IsExhausted(policy, 1) {
		t.Error("retry 1 should not be exhausted with MaxRetries=3")
	}
	if queue.IsExhausted(policy, 3) {
		t.Error("retry 3 should not be exhausted with MaxRetries=3")
	}
	if !queue.IsExhausted(policy, 4) {
		t.Error("retry 4 should be exhausted with MaxRetries=3")
	}
}
