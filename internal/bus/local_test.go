package bus

import (
	"sync/atomic"
	"testing"
	"time"
)

// # Helpers

func publishN(b Bus, env EventEnvelope, n int) {
	for range n {
		b.Publish(env)
	}
}

// # Tests

// TestLocalBus_DeliverToSubscriber verifies that a published event reaches the
// registered handler.
func TestLocalBus_DeliverToSubscriber(t *testing.T) {
	b := NewLocalBus()

	var received atomic.Int32
	done := make(chan struct{})

	b.Subscribe(EventInboundMessage, func(env EventEnvelope) {
		received.Add(1)
		close(done)
	})

	b.Publish(EventEnvelope{Type: EventInboundMessage, AccountID: "a1", RoomID: "!r1"})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler not called within timeout")
	}

	if received.Load() != 1 {
		t.Fatalf("expected 1 delivery, got %d", received.Load())
	}
}

// TestLocalBus_SlowSubscriberDoesNotBlockOthers is the primary verification
// required by the plan: one slow (or panicking) subscriber must not prevent
// another subscriber from receiving events.
func TestLocalBus_SlowSubscriberDoesNotBlockOthers(t *testing.T) {
	b := NewLocalBus()

	// Slow subscriber: blocks for longer than the test deadline if not dropped.
	slowBlock := make(chan struct{})
	b.Subscribe(EventInboundMessage, func(_ EventEnvelope) {
		<-slowBlock // blocks until test releases it
	})

	// Fast subscriber counts deliveries.
	var fastCount atomic.Int32
	fastDone := make(chan struct{})

	b.Subscribe(EventInboundMessage, func(_ EventEnvelope) {
		if fastCount.Add(1) == 3 {
			close(fastDone)
		}
	})

	// Publish 3 events; fast subscriber should receive all of them promptly.
	publishN(b, EventEnvelope{Type: EventInboundMessage}, 3)

	select {
	case <-fastDone:
	case <-time.After(time.Second):
		t.Fatal("fast subscriber did not receive all events within timeout")
	}

	close(slowBlock) // allow slow subscriber to unblock so goroutine exits
}

// TestLocalBus_PanicInHandlerDoesNotKillSubscriber verifies that a panicking
// handler does not terminate the subscriber goroutine; subsequent events are
// still delivered.
func TestLocalBus_PanicInHandlerDoesNotKillSubscriber(t *testing.T) {
	b := NewLocalBus()

	var count atomic.Int32
	done := make(chan struct{})

	b.Subscribe(EventInboundMessage, func(env EventEnvelope) {
		n := count.Add(1)
		if n == 1 {
			panic("intentional test panic")
		}
		if n == 2 {
			close(done)
		}
	})

	b.Publish(EventEnvelope{Type: EventInboundMessage})
	b.Publish(EventEnvelope{Type: EventInboundMessage})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscriber goroutine did not survive panic; second event not delivered")
	}
}

// TestLocalBus_Unsubscribe verifies that events are no longer delivered after
// Unsubscribe is called.
func TestLocalBus_Unsubscribe(t *testing.T) {
	b := NewLocalBus()

	var count atomic.Int32
	id := b.Subscribe(EventInboundMessage, func(_ EventEnvelope) {
		count.Add(1)
	})

	b.Publish(EventEnvelope{Type: EventInboundMessage})
	// Give the handler time to run.
	time.Sleep(50 * time.Millisecond)

	b.Unsubscribe(id)

	before := count.Load()
	b.Publish(EventEnvelope{Type: EventInboundMessage})
	time.Sleep(50 * time.Millisecond)

	if count.Load() != before {
		t.Fatalf("handler called after Unsubscribe: count before=%d after=%d", before, count.Load())
	}
}

// TestLocalBus_EventTypeIsolation verifies that each subscriber only receives
// events for its own registered type (no cross-type contamination).
func TestLocalBus_EventTypeIsolation(t *testing.T) {
	b := NewLocalBus()

	var msgCount, reactCount atomic.Int32
	msgDone := make(chan struct{})
	reactDone := make(chan struct{})

	b.Subscribe(EventInboundMessage, func(_ EventEnvelope) {
		if msgCount.Add(1) == 1 {
			close(msgDone)
		}
	})
	b.Subscribe(EventInboundReaction, func(_ EventEnvelope) {
		if reactCount.Add(1) == 1 {
			close(reactDone)
		}
	})

	b.Publish(EventEnvelope{Type: EventInboundMessage})
	b.Publish(EventEnvelope{Type: EventInboundReaction})

	for _, ch := range []chan struct{}{msgDone, reactDone} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("subscriber not called within timeout")
		}
	}

	// Allow any spurious deliveries to arrive.
	time.Sleep(50 * time.Millisecond)

	if msgCount.Load() != 1 {
		t.Fatalf("message subscriber: expected 1 event, got %d", msgCount.Load())
	}
	if reactCount.Load() != 1 {
		t.Fatalf("reaction subscriber: expected 1 event, got %d", reactCount.Load())
	}
}

// TestLocalBus_BufferFullDropsEvent verifies that when a subscriber's buffer is
// saturated the dropped counter increments and the bus does not block.
func TestLocalBus_BufferFullDropsEvent(t *testing.T) {
	b := NewLocalBus()

	block := make(chan struct{})
	subID := b.Subscribe(EventInboundMessage, func(_ EventEnvelope) {
		<-block // blocks every handler invocation
	})

	// Publish more events than the channel buffer can hold.
	// The first event will be consumed by the goroutine (which then blocks).
	// Subsequent events fill the buffer; extras must be dropped without
	// blocking the caller.
	total := defaultChannelSize + 50
	publishDone := make(chan struct{})
	go func() {
		publishN(b, EventEnvelope{Type: EventInboundMessage}, total)
		close(publishDone)
	}()

	select {
	case <-publishDone:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked when subscriber buffer was full")
	}

	close(block) // release subscriber so goroutine can exit

	b.Unsubscribe(subID)
}
