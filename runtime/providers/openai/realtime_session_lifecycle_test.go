package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// These tests exercise the RealtimeSession's decoupled forwarding pump against a
// fake WebSocket server (no network, no API key): in-order/complete delivery
// under a slow consumer, and the connection-lost terminal-chunk + Done()
// contract. They guard the lifecycle that the barge-in decoupling reworked.

// TestRealtimeSession_PumpDeliversAllChunksInOrder verifies the unbounded pump
// queue forwards every chunk, in order, even when the consumer drains slowly
// (the case that back-pressures the pump's bounded output and exercises the
// internal queue).
func TestRealtimeSession_PumpDeliversAllChunksInOrder(t *testing.T) {
	const n = 30
	audioB64 := base64.StdEncoding.EncodeToString(make([]byte, bargeInDeltaBytes))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if _, _, err := conn.ReadMessage(); err != nil { // session.update
			return
		}
		for i := 0; i < n; i++ {
			if err := conn.WriteJSON(map[string]any{
				"type":          "response.audio.delta",
				"item_id":       "item_resp",
				"response_id":   "resp_1",
				"content_index": i,
				"delta":         audioB64,
			}); err != nil {
				return
			}
		}
		if err := conn.WriteJSON(map[string]any{
			"type":     "response.done",
			"response": map[string]any{"id": "resp_1", "status": "completed"},
		}); err != nil {
			return
		}
		for { // keep the connection open until the client closes it
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{
		endpoint: bargeInWSURL(srv),
	})
	if err != nil {
		t.Fatalf("newRealtimeSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	var indices []int
	gotFinish := false
	for !gotFinish {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				t.Fatal("Response() closed before the finish chunk")
			}
			if chunk.MediaData != nil {
				idx, _ := chunk.Metadata["content_index"].(int)
				indices = append(indices, idx)
			}
			if chunk.FinishReason != nil {
				gotFinish = true
			}
			time.Sleep(2 * time.Millisecond) // slow consumer → back-pressure the pump
		case <-ctx.Done():
			t.Fatalf("timed out after %d audio chunks", len(indices))
		}
	}

	if len(indices) != n {
		t.Fatalf("expected %d audio chunks, got %d", n, len(indices))
	}
	for i, idx := range indices {
		if idx != i {
			t.Fatalf("chunk %d out of order: content_index=%d", i, idx)
		}
	}
}

// TestRealtimeSession_ConnectionLostEmitsTerminalChunk verifies that when the
// server drops the connection, the pump still delivers a terminal chunk tagged
// ErrConnectionLost, closes Response(), and fires Done() afterward — the
// contract the decoupled shutdown path must preserve.
func TestRealtimeSession_ConnectionLostEmitsTerminalChunk(t *testing.T) {
	audioB64 := base64.StdEncoding.EncodeToString(make([]byte, bargeInDeltaBytes))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if err := conn.WriteJSON(map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"},
		}); err != nil {
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // session.update
			return
		}
		_ = conn.WriteJSON(map[string]any{
			"type":          "response.audio.delta",
			"item_id":       "item_resp",
			"response_id":   "resp_1",
			"content_index": 0,
			"delta":         audioB64,
		})
		// Abruptly drop the connection (no close handshake) → the client sees an
		// abnormal closure, which the session treats as connection-lost.
		_ = conn.Close()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{
		endpoint: bargeInWSURL(srv),
	})
	if err != nil {
		t.Fatalf("newRealtimeSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	gotTerminal := false
	for chunk := range session.Response() { // ranges until the pump closes responseCh
		if chunk.Error != nil && errors.Is(chunk.Error, ErrConnectionLost) {
			gotTerminal = true
		}
	}
	if !gotTerminal {
		t.Error("expected a terminal chunk with ErrConnectionLost before Response() closed")
	}

	// Done() must fire after the terminal chunk is delivered.
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Error("Done() did not fire after connection lost")
	}

	// Error() exposes the same distinguishable error.
	if e := session.Error(); e == nil || !errors.Is(e, ErrConnectionLost) {
		t.Errorf("Error() = %v, want ErrConnectionLost", e)
	}
}
