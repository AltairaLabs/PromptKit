package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/gorilla/websocket"
)

// replySetupComplete reads the client's setup message and answers it.
func replySetupComplete(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Errorf("server: read setup: %v", err)
		return
	}
	data, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Errorf("server: write setup_complete: %v", err)
	}
}

// TestStreamSession_ReconnectAbandonsSocketDialedAfterClose covers Close
// landing while reconnect is dialing. The replacement socket must be closed,
// not swapped in: a session that has been closed never closes it again, and
// resending setup on it would restart a conversation the caller ended.
func TestStreamSession_ReconnectAbandonsSocketDialedAfterClose(t *testing.T) {
	var session atomic.Pointer[StreamSession]
	var connCount atomic.Int32
	sessionReady := make(chan struct{})
	// The second connection reports whether the client sent it anything.
	secondGotMessage := make(chan bool, 1)
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := connCount.Add(1)
		if n == 2 {
			// Close the session after reconnect has started dialing but
			// before the dial returns: mark it closed ahead of the upgrade.
			s := session.Load()
			s.mu.Lock()
			s.closed = true
			s.mu.Unlock()
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		switch n {
		case 1:
			replySetupComplete(t, conn)
			<-sessionReady
			// Drop without a close frame: an abnormal closure the session
			// reconnects from.
		case 2:
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _, err := conn.ReadMessage()
			secondGotMessage <- err == nil
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := NewStreamSession(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), "test-key", &StreamSessionConfig{
		AutoReconnect:     true,
		MaxReconnectTries: 1,
	})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	session.Store(s)
	close(sessionReady)

	select {
	case got := <-secondGotMessage:
		if got {
			t.Fatal("reconnect installed and used a socket dialed after Close; want it closed unused")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reconnect never dialed a second connection")
	}
}

// TestStreamSession_SendToolResponses_ReachesServer checks tool results are
// written to the session's current socket as a toolResponse message.
func TestStreamSession_SendToolResponses_ReachesServer(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		replySetupComplete(t, conn)
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(data, &msg)
		received <- msg
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer s.Close()

	if err := s.SendToolResponse(ctx, "call-1", `{"ok":true}`); err != nil {
		t.Fatalf("SendToolResponse failed: %v", err)
	}

	select {
	case msg := <-received:
		toolResp, _ := msg["toolResponse"].(map[string]any)
		fns, _ := toolResp["functionResponses"].([]any)
		if len(fns) != 1 {
			t.Fatalf("want 1 functionResponse, got %v", msg)
		}
		if id := fns[0].(map[string]any)["id"]; id != "call-1" {
			t.Errorf("functionResponse id = %v, want call-1", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received the tool response")
	}
}

// TestStreamSession_SendToolResponses_AfterClose refuses to write to a closed
// session.
func TestStreamSession_SendToolResponses_AfterClose(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		replySetupComplete(t, conn)
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	s, err := NewStreamSession(context.Background(), server.URL(), "test-key", &StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	_ = s.Close()

	err = s.SendToolResponses(context.Background(), []providers.ToolResponse{{ToolCallID: "call-1", Result: "x"}})
	if err == nil || err.Error() != ErrSessionClosed {
		t.Fatalf("SendToolResponses after Close: err = %v, want %q", err, ErrSessionClosed)
	}
}
