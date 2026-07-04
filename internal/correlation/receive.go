package correlation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # Active receive

// activeReceive holds the in-memory state for a pending RegisterReceive call.
type activeReceive struct {
	id        string
	accountID string
	filter    *compiledFilter
	timeoutAt time.Time
	limit     int
	mu        sync.Mutex
	collected []bus.EventEnvelope
	result    chan []bus.EventEnvelope
	done      chan struct{}
	once      sync.Once
}

// RegisterReceive registers a new receive subscription and returns a handle the
// caller blocks on until the timeout elapses or the event limit is reached.
func (m *CorrelationManager) RegisterReceive(ctx context.Context, req ReceiveRequest) (*ReceiveHandle, error) {
	cf, err := newCompiledFilter(&req.Filter)
	if err != nil {
		return nil, err
	}

	id := req.CorrelationID
	if id == "" {
		id = uuid.New().String()
	}

	timeoutAt := time.Now().Add(req.Timeout)

	filterJSON, err := json.Marshal(req.Filter)
	if err != nil {
		return nil, fmt.Errorf("correlation: marshal receive filter: %w", err)
	}
	now := time.Now().UnixMilli()
	roomID := ""
	if req.Filter.RoomID != nil && len(req.Filter.RoomID.Include) == 1 {
		roomID = req.Filter.RoomID.Include[0]
	}
	entry := persistence.CorrelationEntry{
		ID:         id,
		Type:       "receive",
		AccountID:  req.AccountID,
		RoomID:     roomID,
		FilterJSON: string(filterJSON),
		TimeoutAt:  timeoutAt.UnixMilli(),
		State:      "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := m.store.Insert(ctx, entry); err != nil {
		return nil, fmt.Errorf("correlation: persist receive: %w", err)
	}

	r := &activeReceive{
		id:        id,
		accountID: req.AccountID,
		filter:    cf,
		timeoutAt: timeoutAt,
		limit:     req.Limit,
		result:    make(chan []bus.EventEnvelope, 1),
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	m.receives[id] = r
	m.mu.Unlock()

	// Launch timeout goroutine so the window self-closes without polling.
	go func() {
		timer := time.NewTimer(req.Timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			m.resolveReceive(r, "expired")
		case <-r.done:
			// Already resolved by limit or explicit cancel.
		}
	}()

	return &ReceiveHandle{
		ID:     id,
		result: r.result,
		done:   r.done,
	}, nil
}

// collectForReceive appends env to the receive's collected events.
// If the configured limit is reached the subscription is resolved immediately.
func (m *CorrelationManager) collectForReceive(r *activeReceive, env bus.EventEnvelope) {
	r.mu.Lock()
	if r.limit > 0 && len(r.collected) >= r.limit {
		r.mu.Unlock()
		return
	}
	r.collected = append(r.collected, env)
	atLimit := r.limit > 0 && len(r.collected) >= r.limit
	r.mu.Unlock()

	if atLimit {
		go m.resolveReceive(r, "resolved")
	}
}

// resolveReceive closes the receive handle with all collected events.
// Safe to call concurrently; only the first call takes effect.
func (m *CorrelationManager) resolveReceive(r *activeReceive, state string) {
	r.once.Do(func() {
		r.mu.Lock()
		events := make([]bus.EventEnvelope, len(r.collected))
		copy(events, r.collected)
		r.mu.Unlock()

		r.result <- events
		close(r.done)
		m.removeReceive(r.id)

		if state == "resolved" {
			var eventIDs []string
			for _, env := range events {
				var eventID string
				switch p := env.Payload.(type) {
				case bus.InboundMessageEvent:
					eventID = p.EventID
				case bus.InboundReactionEvent:
					eventID = p.EventID
				case bus.InboundEditEvent:
					eventID = p.EventID
				case bus.InboundRedactionEvent:
					eventID = p.EventID
				case bus.InboundMembershipEvent:
					eventID = p.EventID
				case bus.InboundReceiptEvent:
					eventID = p.EventID
				}
				if eventID != "" {
					eventIDs = append(eventIDs, eventID)
				}
			}
			_ = m.store.ResolveCorrelation(context.Background(), r.id, eventIDs)
		} else {
			_ = m.store.UpdateState(context.Background(), r.id, state)
		}
	})
}

// removeReceive deletes the receive subscription from the in-memory map.
func (m *CorrelationManager) removeReceive(id string) {
	m.mu.Lock()
	delete(m.receives, id)
	m.mu.Unlock()
}
