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

// # Active ask

// activeAsk holds the in-memory state for a pending RegisterAsk call.
type activeAsk struct {
	id        string
	accountID string
	filter    *compiledFilter
	timeoutAt time.Time
	result    chan bus.EventEnvelope
	done      chan struct{}
	once      sync.Once
}

// RegisterAsk registers a new ask correlation and returns a handle the caller
// blocks on until a matching inbound event arrives or the timeout elapses.
//
// When req.Filter.InReplyTo is empty and req.OutboundEventID is non-empty, the
// ask defaults to matching events that carry an m.in_reply_to relation targeting
// OutboundEventID - the preferred correlation strategy.
func (m *CorrelationManager) RegisterAsk(ctx context.Context, req AskRequest) (*AskHandle, error) {
	// Default InReplyTo to the outbound event ID.
	if req.Filter.InReplyTo == "" && req.OutboundEventID != "" {
		req.Filter.InReplyTo = req.OutboundEventID
	}

	// Auto-exclude the bot's own sender so the echo of the sent message never
	// resolves the ask. Append to any existing exclude list the caller supplied.
	if req.BotUserID != "" {
		if req.Filter.SenderID == nil {
			req.Filter.SenderID = &StringSetFilter{}
		}
		req.Filter.SenderID.Exclude = append(req.Filter.SenderID.Exclude, req.BotUserID)
	}

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
		return nil, fmt.Errorf("correlation: marshal ask filter: %w", err)
	}
	now := time.Now().UnixMilli()
	roomID := ""
	if req.Filter.RoomID != nil && len(req.Filter.RoomID.Include) == 1 {
		roomID = req.Filter.RoomID.Include[0]
	}
	entry := persistence.CorrelationEntry{
		ID:              id,
		Type:            "ask",
		AccountID:       req.AccountID,
		RoomID:          roomID,
		OutboundEventID: req.OutboundEventID,
		FilterJSON:      string(filterJSON),
		TimeoutAt:       timeoutAt.UnixMilli(),
		State:           "pending",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := m.store.Insert(ctx, entry); err != nil {
		return nil, fmt.Errorf("correlation: persist ask: %w", err)
	}

	a := &activeAsk{
		id:        id,
		accountID: req.AccountID,
		filter:    cf,
		timeoutAt: timeoutAt,
		result:    make(chan bus.EventEnvelope, 1),
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	m.asks[id] = a
	m.mu.Unlock()

	// Launch timeout goroutine so the handle self-expires without polling.
	go func() {
		timer := time.NewTimer(req.Timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			m.resolveAskTimeout(a)
		case <-a.done:
			// Already resolved by a matching event.
		}
	}()

	return &AskHandle{
		ID:     id,
		result: a.result,
		done:   a.done,
	}, nil
}

// deliverToAsk resolves an ask with a matched event. Safe to call concurrently;
// only the first call takes effect.
func (m *CorrelationManager) deliverToAsk(a *activeAsk, env bus.EventEnvelope) {
	a.once.Do(func() {
		a.result <- env
		close(a.done)
		m.removeAsk(a.id)

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
			_ = m.store.ResolveCorrelation(context.Background(), a.id, []string{eventID})
		} else {
			_ = m.store.UpdateState(context.Background(), a.id, "resolved")
		}
	})
}

// resolveAskTimeout expires an ask that did not receive a matching event within
// its timeout window.
func (m *CorrelationManager) resolveAskTimeout(a *activeAsk) {
	a.once.Do(func() {
		close(a.result)
		close(a.done)
		m.removeAsk(a.id)
		_ = m.store.UpdateState(context.Background(), a.id, "expired")
	})
}

// removeAsk deletes the ask from the in-memory map.
func (m *CorrelationManager) removeAsk(id string) {
	m.mu.Lock()
	delete(m.asks, id)
	m.mu.Unlock()
}
