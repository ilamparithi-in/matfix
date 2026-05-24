package correlation

import (
	"log/slog"
	"sync"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/persistence"

	"context"
)

// # Config

// Config holds the construction parameters for a CorrelationManager.
type Config struct {
	Bus   bus.Bus
	Store persistence.CorrelationStore
}

// # CorrelationManager

// CorrelationManager coordinates ask and receive subscriptions against the
// inbound event stream. It subscribes to all inbound bus event types and routes
// matching events to registered handles.
//
// RegisterAsk and RegisterReceive are the two entry points for callers. Both
// return handles that the caller blocks on via Wait.
type CorrelationManager struct {
	bus   bus.Bus
	store persistence.CorrelationStore

	mu       sync.RWMutex
	asks     map[string]*activeAsk
	receives map[string]*activeReceive

	subIDs []bus.SubscriptionID
}

// New constructs a CorrelationManager from cfg.
func New(cfg Config) *CorrelationManager {
	return &CorrelationManager{
		bus:      cfg.Bus,
		store:    cfg.Store,
		asks:     make(map[string]*activeAsk),
		receives: make(map[string]*activeReceive),
	}
}

// Start subscribes to all inbound bus event types and launches the cleanup
// goroutine. It must be called at most once. It returns when ctx is cancelled.
func (m *CorrelationManager) Start(ctx context.Context) {
	inboundTypes := []bus.EventType{
		bus.EventInboundMessage,
		bus.EventInboundReaction,
		bus.EventInboundEdit,
		bus.EventInboundRedaction,
		bus.EventInboundMembership,
		bus.EventInboundReceipt,
	}
	for _, et := range inboundTypes {
		id := m.bus.Subscribe(et, m.handleEvent)
		m.subIDs = append(m.subIDs, id)
	}

	go m.runCleanup(ctx)

	// Unsubscribe automatically when the context is cancelled.
	go func() {
		<-ctx.Done()
		m.Stop()
	}()
}

// Stop unsubscribes all bus subscriptions.
func (m *CorrelationManager) Stop() {
	for _, id := range m.subIDs {
		m.bus.Unsubscribe(id)
	}
}

// handleEvent is the bus handler for all subscribed inbound event types.
// It routes matching events to registered ask and receive handles.
func (m *CorrelationManager) handleEvent(env bus.EventEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("correlation: panic in event handler", "panic", r)
		}
	}()

	// Collect matches under a read lock to avoid holding the lock during delivery.
	m.mu.RLock()
	var matchedAsks []*activeAsk
	for _, a := range m.asks {
		if a.filter.matches(env, a.accountID) {
			matchedAsks = append(matchedAsks, a)
		}
	}
	var matchedReceives []*activeReceive
	for _, r := range m.receives {
		if r.filter.matches(env, r.accountID) {
			matchedReceives = append(matchedReceives, r)
		}
	}
	m.mu.RUnlock()

	for _, a := range matchedAsks {
		m.deliverToAsk(a, env)
	}
	for _, r := range matchedReceives {
		m.collectForReceive(r, env)
	}
}
