package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// newToolCaptureServer returns a fake Realtime WebSocket server that completes
// the handshake, drains the client's session.update, then forwards every
// subsequent client event (decoded JSON) to received in order.
func newToolCaptureServer(t *testing.T, received chan<- map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"},
		}); err != nil {
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // drain session.update
			return
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}
			received <- m
		}
	}))
}

func recvEvent(t *testing.T, received <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case m := <-received:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a client event")
		return nil
	}
}

func openToolSession(t *testing.T, srv *httptest.Server) *RealtimeSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{
		endpoint: bargeInWSURL(srv),
	})
	if err != nil {
		t.Fatalf("newRealtimeSession: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestSendToolResponse_EmitsFunctionOutputThenResponseCreate(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newToolCaptureServer(t, received)
	defer srv.Close()

	session := openToolSession(t, srv)

	if err := session.SendToolResponse(context.Background(), "call_123", "the result"); err != nil {
		t.Fatalf("SendToolResponse: %v", err)
	}

	// 1. conversation.item.create carrying a function_call_output item.
	create := recvEvent(t, received)
	if create["type"] != "conversation.item.create" {
		t.Fatalf("first event type = %v, want conversation.item.create", create["type"])
	}
	item, ok := create["item"].(map[string]any)
	if !ok {
		t.Fatalf("event missing item object: %+v", create)
	}
	if item["type"] != "function_call_output" {
		t.Errorf("item.type = %v, want function_call_output", item["type"])
	}
	if item["call_id"] != "call_123" {
		t.Errorf("item.call_id = %v, want call_123", item["call_id"])
	}
	if item["output"] != "the result" {
		t.Errorf("item.output = %v, want 'the result'", item["output"])
	}

	// 2. response.create to continue the turn.
	resp := recvEvent(t, received)
	if resp["type"] != "response.create" {
		t.Errorf("second event type = %v, want response.create", resp["type"])
	}
}

func TestSendToolResponses_MultipleOutputsThenOneResponseCreate(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newToolCaptureServer(t, received)
	defer srv.Close()

	session := openToolSession(t, srv)

	responses := []providers.ToolResponse{
		{ToolCallID: "call_a", Result: "res a"},
		{ToolCallID: "call_b", Result: "res b"},
	}
	if err := session.SendToolResponses(context.Background(), responses); err != nil {
		t.Fatalf("SendToolResponses: %v", err)
	}

	// Two function_call_output items, in order, then a single response.create.
	for _, want := range responses {
		ev := recvEvent(t, received)
		if ev["type"] != "conversation.item.create" {
			t.Fatalf("event type = %v, want conversation.item.create", ev["type"])
		}
		item := ev["item"].(map[string]any)
		if item["call_id"] != want.ToolCallID {
			t.Errorf("item.call_id = %v, want %v", item["call_id"], want.ToolCallID)
		}
		if item["output"] != want.Result {
			t.Errorf("item.output = %v, want %v", item["output"], want.Result)
		}
	}
	final := recvEvent(t, received)
	if final["type"] != "response.create" {
		t.Errorf("final event type = %v, want response.create", final["type"])
	}
}

func TestSendToolResponse_ClosedSessionErrors(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newToolCaptureServer(t, received)
	defer srv.Close()

	session := openToolSession(t, srv)
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := session.SendToolResponse(context.Background(), "call_1", "x"); err == nil {
		t.Error("SendToolResponse on closed session should error")
	}
	err := session.SendToolResponses(context.Background(), []providers.ToolResponse{{ToolCallID: "call_1", Result: "x"}})
	if err == nil {
		t.Error("SendToolResponses on closed session should error")
	}
}
