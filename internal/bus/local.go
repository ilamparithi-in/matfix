package bus

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

// defaultChannelSize is the per-subscriber event buffer depth.
// Events published to a full buffer are dropped for that subscriber.
const defaultChannelSize = 256

// # Subscription entry

type subscription struct {
	id        SubscriptionID
	eventType EventType
	ch        chan EventEnvelope
	dropped   atomic.Int64
}

// # LocalBus

// LocalBus is the in-process implementation of Bus.
// Each subscriber gets its own buffered channel and a dedicated goroutine.
// Failure or slowness of one subscriber never blocks or panics others.
type LocalBus struct {
	mu     sync.RWMutex
	subs   map[SubscriptionID]*subscription
	byType map[EventType][]*subscription
}

// NewLocalBus creates a ready-to-use LocalBus.
func NewLocalBus() *LocalBus {
	return &LocalBus{
		subs:   make(map[SubscriptionID]*subscription),
		byType: make(map[EventType][]*subscription),
	}
}

// Publish dispatches env to every subscriber registered for env.Type.
// The RLock is held for the duration of dispatch so that Unsubscribe cannot
// close a channel while a send is in progress.
func (b *LocalBus) Publish(env EventEnvelope) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, s := range b.byType[env.Type] {
		select {
		case s.ch <- env:
		default:
			n := s.dropped.Add(1)
			slog.Warn("bus: subscriber buffer full, event dropped",
				"event_type", env.Type,
				"account_id", env.AccountID,
				"subscription_id", s.id,
				"total_dropped", n,
			)
		}
	}
}

// Subscribe registers handler for eventType and starts its dispatch goroutine.
func (b *LocalBus) Subscribe(eventType EventType, handler HandlerFunc) SubscriptionID {
	id := SubscriptionID(uuid.New().String())
	s := &subscription{
		id:        id,
		eventType: eventType,
		ch:        make(chan EventEnvelope, defaultChannelSize),
	}

	b.mu.Lock()
	b.subs[id] = s
	b.byType[eventType] = append(b.byType[eventType], s)
	b.mu.Unlock()

	go runSubscriber(s, handler)
	return id
}

// Unsubscribe removes the subscription and closes its channel, which causes
// the dispatch goroutine to exit cleanly.
// The WriteLock is held while closing the channel to prevent a concurrent
// Publish from sending to a channel that is about to be closed.
func (b *LocalBus) Unsubscribe(id SubscriptionID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	s, ok := b.subs[id]
	if !ok {
		return
	}

	delete(b.subs, id)

	list := b.byType[s.eventType]
	for i, entry := range list {
		if entry.id == id {
			b.byType[s.eventType] = append(list[:i], list[i+1:]...)
			break
		}
	}

	close(s.ch)
}

// # Subscriber dispatch loop

// runSubscriber drains s.ch and calls handler for each event.
// It recovers from handler panics so that one misbehaving subscriber cannot
// terminate the goroutine.
func runSubscriber(s *subscription, handler HandlerFunc) {
	for env := range s.ch {
		safeCall(s.id, handler, env)
	}
}

// safeCall calls handler inside a deferred recover so panics are logged rather
// than propagated.
func safeCall(id SubscriptionID, handler HandlerFunc, env EventEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("bus: subscriber handler panicked",
				"subscription_id", id,
				"event_type", env.Type,
				"panic", r,
			)
		}
	}()
	handler(env)
}
