package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apires "github.com/ilamparithi-in/matfix/internal/api/response"
	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/correlation"
)

func TestReceiveHandler_HTTPDelivery(t *testing.T) {
	b := bus.NewLocalBus()
	corr := correlation.New(correlation.Config{Bus: b})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	corr.Start(ctx)

	handler := receiveHandler(corr)

	reqBody := []byte(`{
		"account_id": "bot1",
		"timeout_seconds": 2,
		"limit": 1,
		"filter": {
			"room_id": { "include": ["!roomA:example.com"] },
			"body_regex": "^!deploy.*$"
		}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/receive", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()

	// In background, verify active count increases and then publish event
	go func() {
		// Wait until handler registers subscription in memory
		for i := 0; i < 50; i++ {
			_, receives := corr.ActiveCounts()
			if receives == 1 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}

		// Publish matching event
		b.Publish(bus.EventEnvelope{
			Type:      bus.EventInboundMessage,
			AccountID: "bot1",
			RoomID:    "!roomA:example.com",
			Payload: bus.InboundMessageEvent{
				EventID:  "$event123",
				SenderID: "@alice:example.com",
				Body:     "!deploy production",
			},
		})
	}()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp apires.ReceiveResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if resp.TimedOut {
		t.Fatal("expected timed_out=false, got true")
	}
	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Events))
	}
	ev := resp.Events[0]
	if ev.EventID != "$event123" || ev.Body != "!deploy production" || ev.RoomID != "!roomA:example.com" {
		t.Fatalf("unexpected event payload: %+v", ev)
	}
}

func TestAdminSubscriptionsHandler_HTTPCounts(t *testing.T) {
	b := bus.NewLocalBus()
	corr := correlation.New(correlation.Config{Bus: b})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	corr.Start(ctx)

	// Admin handler with nil store: purely in-memory
	handler := adminSubscriptionsHandler(corr, nil)

	// Step 1: Initial state -> 0, 0
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/subscriptions", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp apires.AdminSubscriptionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Receives != 0 || resp.Asks != 0 {
		t.Fatalf("expected 0/0, got %d/%d", resp.Asks, resp.Receives)
	}

	// Step 2: Register in-memory receive -> should report 1 receive
	handle, err := corr.RegisterReceive(ctx, correlation.ReceiveRequest{
		AccountID: "bot1",
		Timeout:   1 * time.Second,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("RegisterReceive: %v", err)
	}

	rec = httptest.NewRecorder()
	handler(rec, req)
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Receives != 1 || resp.Asks != 0 {
		t.Fatalf("expected 0 asks, 1 receive; got %d/%d", resp.Asks, resp.Receives)
	}

	// Step 3: Resolve handle -> should drop back to 0
	b.Publish(bus.EventEnvelope{
		Type:      bus.EventInboundMessage,
		AccountID: "bot1",
		RoomID:    "!room:example.com",
		Payload: bus.InboundMessageEvent{
			EventID:  "$evt",
			SenderID: "@user:example.com",
			Body:     "any",
		},
	})
	_, _ = handle.Wait(ctx)

	time.Sleep(10 * time.Millisecond)
	rec = httptest.NewRecorder()
	handler(rec, req)
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Receives != 0 {
		t.Fatalf("expected 0 receives after resolution, got %d", resp.Receives)
	}
}
