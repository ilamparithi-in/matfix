package correlation

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// # Active receive

// activeReceive holds the in-memory state for a pending RegisterReceive call.
type activeReceive struct {
	id        string
	accountID string
	rooms     []string
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
// Receive subscriptions are kept purely in-memory and indexed by room.
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

	var rooms []string
	if req.Filter.RoomID != nil && len(req.Filter.RoomID.Include) > 0 {
		rooms = req.Filter.RoomID.Include
	}

	r := &activeReceive{
		id:        id,
		accountID: req.AccountID,
		rooms:     rooms,
		filter:    cf,
		timeoutAt: timeoutAt,
		limit:     req.Limit,
		result:    make(chan []bus.EventEnvelope, 1),
		done:      make(chan struct{}),
	}

	m.addReceive(r)

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

		m.removeReceive(r)
		r.result <- events
		close(r.done)
	})
}

// addReceive registers the receive subscription in memory and in the room index.
func (m *CorrelationManager) addReceive(r *activeReceive) {
	m.receivesMu.Lock()
	defer m.receivesMu.Unlock()

	m.receives[r.id] = r
	if len(r.rooms) == 0 {
		m.wildcardReceives[r.id] = r
	} else {
		for _, rid := range r.rooms {
			bucket, ok := m.receivesByRoom[rid]
			if !ok {
				bucket = make(map[string]*activeReceive)
				m.receivesByRoom[rid] = bucket
			}
			bucket[r.id] = r
		}
	}
}

// removeReceive deletes the receive subscription from the in-memory map and room index.
func (m *CorrelationManager) removeReceive(r *activeReceive) {
	m.receivesMu.Lock()
	defer m.receivesMu.Unlock()

	delete(m.receives, r.id)
	if len(r.rooms) == 0 {
		delete(m.wildcardReceives, r.id)
	} else {
		for _, rid := range r.rooms {
			if bucket, ok := m.receivesByRoom[rid]; ok {
				delete(bucket, r.id)
				if len(bucket) == 0 {
					delete(m.receivesByRoom, rid)
				}
			}
		}
	}
}
