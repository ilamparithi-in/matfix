package correlation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// explodingStore strictly fails if any correlation database method is called.
type explodingStore struct {
	t *testing.T
}

func (e *explodingStore) Insert(ctx context.Context, entry persistence.CorrelationEntry) error {
	e.t.Fatalf("explodingStore: Insert called on database: %+v", entry)
	return nil
}
func (e *explodingStore) GetByID(ctx context.Context, id string) (*persistence.CorrelationEntry, error) {
	e.t.Fatalf("explodingStore: GetByID called on database: %s", id)
	return nil, nil
}
func (e *explodingStore) UpdateState(ctx context.Context, id, state string) error {
	e.t.Fatalf("explodingStore: UpdateState called on database: %s, %s", id, state)
	return nil
}
func (e *explodingStore) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	// DeleteExpired may be called by background cleanup for asks, which is allowed to return 0.
	return 0, nil
}
func (e *explodingStore) ListActive(ctx context.Context) ([]persistence.CorrelationEntry, error) {
	e.t.Fatalf("explodingStore: ListActive called on database")
	return nil, nil
}

func TestReceive_ZeroDatabaseAccess(t *testing.T) {
	b := bus.NewLocalBus()
	// Use explodingStore: if ANY receive operation touches the DB, test immediately fails
	store := &explodingStore{t: t}
	mgr := New(Config{Bus: b, Store: store})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	// 1. Register
	handle, err := mgr.RegisterReceive(ctx, ReceiveRequest{
		AccountID: "acc1",
		Filter: FilterNode{
			RoomID: &StringSetFilter{Include: []string{"!roomA:example.com"}},
		},
		Timeout: 500 * time.Millisecond,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("RegisterReceive failed: %v", err)
	}

	asks, receives := mgr.ActiveCounts()
	if asks != 0 || receives != 1 {
		t.Fatalf("expected 0 asks, 1 receive; got %d asks, %d receives", asks, receives)
	}

	// 2. Deliver event
	b.Publish(bus.EventEnvelope{
		Type:      bus.EventInboundMessage,
		AccountID: "acc1",
		RoomID:    "!roomA:example.com",
		Payload: bus.InboundMessageEvent{
			EventID:  "$msg_zero_db",
			SenderID: "@user:example.com",
			Body:     "zero db test",
		},
	})

	// 3. Resolve
	events, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if len(events) != 1 || events[0].RoomID != "!roomA:example.com" {
		t.Fatalf("unexpected events: %+v", events)
	}

	// 4. Timeout case with zero DB
	timeoutHandle, err := mgr.RegisterReceive(ctx, ReceiveRequest{
		AccountID: "acc1",
		Timeout:   50 * time.Millisecond,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("RegisterReceive for timeout failed: %v", err)
	}
	timeoutEvents, err := timeoutHandle.Wait(ctx)
	if err != nil {
		t.Fatalf("timeout Wait failed: %v", err)
	}
	if len(timeoutEvents) != 0 {
		t.Fatalf("expected 0 events, got %d", len(timeoutEvents))
	}
}

func TestReceive_MultiRoomAndWildcardIsolation(t *testing.T) {
	b := bus.NewLocalBus()
	mgr := New(Config{Bus: b})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	// Sub 1: Multi-room (!room1 and !room2)
	hMulti, err := mgr.RegisterReceive(ctx, ReceiveRequest{
		AccountID: "acc1",
		Filter: FilterNode{
			RoomID: &StringSetFilter{Include: []string{"!room1:example.com", "!room2:example.com"}},
		},
		Timeout: 1 * time.Second,
		Limit:   2,
	})
	if err != nil {
		t.Fatalf("RegisterReceive hMulti failed: %v", err)
	}

	// Sub 2: Single room (!room1 only)
	hSingle, err := mgr.RegisterReceive(ctx, ReceiveRequest{
		AccountID: "acc1",
		Filter: FilterNode{
			RoomID: &StringSetFilter{Include: []string{"!room1:example.com"}},
		},
		Timeout: 200 * time.Millisecond,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("RegisterReceive hSingle failed: %v", err)
	}

	// Sub 3: Room 3 only (!room3)
	hRoom3, err := mgr.RegisterReceive(ctx, ReceiveRequest{
		AccountID: "acc1",
		Filter: FilterNode{
			RoomID: &StringSetFilter{Include: []string{"!room3:example.com"}},
		},
		Timeout: 200 * time.Millisecond,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("RegisterReceive hRoom3 failed: %v", err)
	}

	// Publish to room 2:
	// - hMulti should get it (it includes room 2)
	// - hSingle (!room1 only) MUST NOT get it
	// - hRoom3 (!room3 only) MUST NOT get it
	b.Publish(bus.EventEnvelope{
		Type:      bus.EventInboundMessage,
		AccountID: "acc1",
		RoomID:    "!room2:example.com",
		Payload: bus.InboundMessageEvent{
			EventID:  "$ev_room2",
			SenderID: "@user:example.com",
			Body:     "room 2 message",
		},
	})

	// Publish to room 1:
	// - hMulti should get it (second event, completes limit: 2)
	// - hSingle should get it (completes limit: 1)
	// - hRoom3 MUST NOT get it
	b.Publish(bus.EventEnvelope{
		Type:      bus.EventInboundMessage,
		AccountID: "acc1",
		RoomID:    "!room1:example.com",
		Payload: bus.InboundMessageEvent{
			EventID:  "$ev_room1",
			SenderID: "@user:example.com",
			Body:     "room 1 message",
		},
	})

	// Verify hSingle received only room 1
	singleEv, err := hSingle.Wait(ctx)
	if err != nil || len(singleEv) != 1 {
		t.Fatalf("hSingle failed: err=%v, len=%d", err, len(singleEv))
	}
	if singleEv[0].RoomID != "!room1:example.com" || singleEv[0].Payload.(bus.InboundMessageEvent).EventID != "$ev_room1" {
		t.Fatalf("hSingle got wrong event: %+v", singleEv[0])
	}

	// Verify hMulti received both room 2 and room 1
	multiEv, err := hMulti.Wait(ctx)
	if err != nil || len(multiEv) != 2 {
		t.Fatalf("hMulti failed: err=%v, len=%d", err, len(multiEv))
	}
	if multiEv[0].RoomID != "!room2:example.com" || multiEv[1].RoomID != "!room1:example.com" {
		t.Fatalf("hMulti got unexpected events order/rooms: %+v", multiEv)
	}

	// Verify hRoom3 timed out with 0 events
	room3Ev, err := hRoom3.Wait(ctx)
	if err != nil || len(room3Ev) != 0 {
		t.Fatalf("hRoom3 should have timed out with 0 events, got: len=%d, err=%v", len(room3Ev), err)
	}

	// Verify room bucket cleanup left no dangling references
	mgr.receivesMu.RLock()
	for rID, bucket := range mgr.receivesByRoom {
		if len(bucket) != 0 {
			t.Errorf("dangling bucket for room %s: len=%d", rID, len(bucket))
		}
	}
	if len(mgr.receives) != 0 {
		t.Errorf("receives map not empty: len=%d", len(mgr.receives))
	}
	mgr.receivesMu.RUnlock()
}

func TestReceive_HighConcurrencyStrictDelivery(t *testing.T) {
	b := bus.NewLocalBus()
	mgr := New(Config{Bus: b})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	const numRooms = 10
	const clientsPerRoom = 10
	const totalClients = numRooms * clientsPerRoom

	type clientResult struct {
		clientIdx int
		targetRoom string
		events     []bus.EventEnvelope
		err        error
	}

	results := make([]clientResult, totalClients)
	var wg sync.WaitGroup
	wg.Add(totalClients)

	readyCh := make(chan struct{})
	var readyCount atomic.Int32

	for i := 0; i < totalClients; i++ {
		roomIdx := i % numRooms
		targetRoom := fmt.Sprintf("!room_%d:example.com", roomIdx)
		idx := i

		go func(cIdx int, room string) {
			defer wg.Done()
			h, err := mgr.RegisterReceive(ctx, ReceiveRequest{
				AccountID: "acc1",
				Filter: FilterNode{
					RoomID:    &StringSetFilter{Include: []string{room}},
					BodyRegex: `^msg_for_.*$`,
				},
				Timeout: 1 * time.Second,
				Limit:   1,
			})
			if err != nil {
				results[cIdx] = clientResult{clientIdx: cIdx, targetRoom: room, err: err}
				return
			}

			// Signal ready
			if readyCount.Add(1) == int32(totalClients) {
				close(readyCh)
			}
			<-readyCh

			ev, waitErr := h.Wait(ctx)
			results[cIdx] = clientResult{
				clientIdx:  cIdx,
				targetRoom: room,
				events:     ev,
				err:        waitErr,
			}
		}(idx, targetRoom)
	}

	// Wait for all receivers to be registered
	<-readyCh
	time.Sleep(20 * time.Millisecond) // ensure all are waiting in handle.Wait

	// Publish exactly 1 message per room
	for r := 0; r < numRooms; r++ {
		room := fmt.Sprintf("!room_%d:example.com", r)
		b.Publish(bus.EventEnvelope{
			Type:      bus.EventInboundMessage,
			AccountID: "acc1",
			RoomID:    room,
			Payload: bus.InboundMessageEvent{
				EventID:  fmt.Sprintf("$evt_room_%d", r),
				SenderID: "@user:example.com",
				Body:     fmt.Sprintf("msg_for_room_%d", r),
			},
		})
	}

	wg.Wait()

	// Strict assertions on all client results
	for i, res := range results {
		if res.err != nil {
			t.Fatalf("client %d returned error: %v", i, res.err)
		}
		if len(res.events) != 1 {
			t.Fatalf("client %d (target: %s) expected 1 event, got %d", i, res.targetRoom, len(res.events))
		}
		ev := res.events[0]
		// Verify strict room isolation
		if ev.RoomID != res.targetRoom {
			t.Fatalf("LEAK: client %d targeted room %s, but received event for room %s", i, res.targetRoom, ev.RoomID)
		}
		msg, ok := ev.Payload.(bus.InboundMessageEvent)
		if !ok {
			t.Fatalf("client %d payload not InboundMessageEvent", i)
		}
		expectedBody := fmt.Sprintf("msg_for_%s", res.targetRoom[1:len(res.targetRoom)-12])
		if msg.Body != expectedBody {
			t.Fatalf("client %d got body %q, want %q", i, msg.Body, expectedBody)
		}
	}

	// Verify all subscriptions cleaned up
	time.Sleep(20 * time.Millisecond)
	_, receives := mgr.ActiveCounts()
	if receives != 0 {
		t.Fatalf("expected 0 active receives, got %d", receives)
	}
}
