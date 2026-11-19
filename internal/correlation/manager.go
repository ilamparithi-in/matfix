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

	// Dedicated lock and bucketed index for receives
	receivesMu       sync.RWMutex
	receives         map[string]*activeReceive
	receivesByRoom   map[string]map[string]*activeReceive
	wildcardReceives map[string]*activeReceive

	// Dedicated lock for asks
	asksMu sync.RWMutex
	asks   map[string]*activeAsk

	subIDs []bus.SubscriptionID
}

// New constructs a CorrelationManager from cfg.
func New(cfg Config) *CorrelationManager {
	return &CorrelationManager{
		bus:              cfg.Bus,
		store:            cfg.Store,
		asks:             make(map[string]*activeAsk),
		receives:         make(map[string]*activeReceive),
		receivesByRoom:   make(map[string]map[string]*activeReceive),
		wildcardReceives: make(map[string]*activeReceive),
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

	// Collect matched asks under asksMu
	m.asksMu.RLock()
	var matchedAsks []*activeAsk
	for _, a := range m.asks {
		// Never match the homeserver echo of the bot's own sent message.
		// The ask's correlation ID is the sent Matrix event ID, so an inbound
		// message carrying that same event ID is always the echo — reject it
		// regardless of what the sender field contains.
		if p, ok := env.Payload.(bus.InboundMessageEvent); ok && p.EventID == a.id {
			continue
		}
		if a.filter.matches(env, a.accountID) {
			matchedAsks = append(matchedAsks, a)
		}
	}
	m.asksMu.RUnlock()

	// Collect matched receives under receivesMu using room index
	m.receivesMu.RLock()
	var matchedReceives []*activeReceive

	// 1. Check room-specific bucket if room ID is present
	if env.RoomID != "" {
		if roomBucket, ok := m.receivesByRoom[env.RoomID]; ok {
			for _, r := range roomBucket {
				if r.filter.matches(env, r.accountID) {
					matchedReceives = append(matchedReceives, r)
				}
			}
		}
	}

	// 2. Always check wildcard bucket (subscriptions with no room filter)
	for _, r := range m.wildcardReceives {
		if r.filter.matches(env, r.accountID) {
			matchedReceives = append(matchedReceives, r)
		}
	}
	m.receivesMu.RUnlock()

	for _, a := range matchedAsks {
		m.deliverToAsk(a, env)
	}
	for _, r := range matchedReceives {
		m.collectForReceive(r, env)
	}
}

// ActiveCounts returns the count of currently active in-memory ask and receive subscriptions.
func (m *CorrelationManager) ActiveCounts() (asks int, receives int) {
	m.asksMu.RLock()
	asks = len(m.asks)
	m.asksMu.RUnlock()

	m.receivesMu.RLock()
	receives = len(m.receives)
	m.receivesMu.RUnlock()

	return asks, receives
}
