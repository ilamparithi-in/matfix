package worker

import (
	"context"
	"sync"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/queue"
)

// # ClientLookup

// ClientLookup returns the engine.Client for the given accountID.
// Returns false when the account is not currently available.
type ClientLookup func(accountID string) (*engine.Client, bool)

// # Config

// Config holds the construction parameters for a WorkerPool.
type Config struct {
	// Accounts is the ordered list of account IDs the workers will service.
	Accounts    []string
	Manager     *queue.QueueManager
	Clients     ClientLookup
	Bus         bus.Bus
	Concurrency int // total worker goroutines
}

// # WorkerPool

// WorkerPool runs a fixed number of goroutines that pull jobs from the
// QueueManager, deliver them via the Matrix Engine, and report outcomes back to
// the QueueManager and the Event Bus.
//
// Each worker fails independently; a panic in one worker is recovered and that
// goroutine restarts its loop without affecting others.
type WorkerPool struct {
	cfg    Config
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New constructs a WorkerPool. Concurrency is set to 1 when cfg.Concurrency < 1.
func New(cfg Config) *WorkerPool {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	return &WorkerPool{cfg: cfg}
}

// Start launches cfg.Concurrency worker goroutines. The pool runs until Stop
// is called or the parent ctx is cancelled. Start must be called at most once.
func (p *WorkerPool) Start(ctx context.Context) {
	ctx, p.cancel = context.WithCancel(ctx)
	for i := 0; i < p.cfg.Concurrency; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			runWorker(ctx, p.cfg)
		}()
	}
}

// Stop signals all workers to stop and waits for them to exit.
func (p *WorkerPool) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}
