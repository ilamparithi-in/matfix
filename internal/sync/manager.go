package sync

import (
	"context"

	"maunium.net/go/mautrix/event"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # Decrypter

// Decrypter can decrypt m.room.encrypted events into their plaintext form.
// The CryptoManager (Phase 6) implements this interface.
// A nil Decrypter causes m.room.encrypted events to be silently dropped.
type Decrypter interface {
	DecryptMegolmEvent(ctx context.Context, evt *event.Event) (*event.Event, error)
}

// # Config

// Config holds all dependencies needed to construct a SyncManager.
type Config struct {
	AccountID  string
	Client     *engine.Client
	SyncStore  persistence.SyncStore
	CacheStore persistence.EventCacheStore
	Bus        bus.Bus
	// Decrypter is optional. If nil, encrypted events are dropped.
	Decrypter Decrypter
}

// # SyncManager

// SyncManager owns the /sync loop for one Matrix account.
// It is the only component permitted to call /sync or advance the sync token.
type SyncManager struct {
	accountID  string
	client     *engine.Client
	syncStore  persistence.SyncStore
	cacheStore persistence.EventCacheStore
	bus        bus.Bus
	decrypter  Decrypter

	cancel context.CancelFunc
}

// New constructs a SyncManager from cfg.
func New(cfg Config) *SyncManager {
	return &SyncManager{
		accountID:  cfg.AccountID,
		client:     cfg.Client,
		syncStore:  cfg.SyncStore,
		cacheStore: cfg.CacheStore,
		bus:        cfg.Bus,
		decrypter:  cfg.Decrypter,
	}
}

// Start launches the /sync loop in a background goroutine.
// The loop runs until ctx is cancelled or Stop is called.
func (m *SyncManager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)
	go m.run(ctx)
}

// Stop signals the sync loop to terminate.
func (m *SyncManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}
