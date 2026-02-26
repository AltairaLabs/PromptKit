package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// mockWebSocketServer creates a test WebSocket server
type mockWebSocketServer struct {
	server   *httptest.Server
	upgrader websocket.Upgrader
	handler  func(*websocket.Conn)
}

func newMockWebSocketServer(handler func(*websocket.Conn)) *mockWebSocketServer {
	mws := &mockWebSocketServer{
		upgrader: websocket.Upgrader{},
		handler:  handler,
	}

	mws.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := mws.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		if mws.handler != nil {
			mws.handler(conn)
		}
	}))

	return mws
}

func (mws *mockWebSocketServer) Close() {
	mws.server.Close()
}

func (mws *mockWebSocketServer) URL() string {
	return "ws" + strings.TrimPrefix(mws.server.URL, "http")
}

func TestNewWebSocketManager(t *testing.T) {
	wm := NewWebSocketManager("ws://test.example.com", "test-api-key")

	if wm.url != "ws://test.example.com" {
		t.Errorf("Expected url 'ws://test.example.com', got '%s'", wm.url)
	}

	if wm.apiKey != "test-api-key" {
		t.Errorf("Expected apiKey 'test-api-key', got '%s'", wm.apiKey)
	}

	if wm.IsConnected() {
		t.Error("Expected not connected initially")
	}
}

func TestWebSocketManager_Connect(t *testing.T) {
	// Create mock server
	var connected atomic.Bool
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		connected.Store(true)
		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	// Create manager and connect
	wm := NewWebSocketManager(server.URL(), "test-key")
	ctx := context.Background()

	err := wm.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !wm.IsConnected() {
		t.Error("Expected connected after Connect()")
	}

	// Wait briefly for handler to be called (async connection establishment)
	time.Sleep(50 * time.Millisecond)

	if !connected.Load() {
		t.Error("Server handler was not called")
	}

	_ = wm.Close()
}

func TestWebSocketManager_Connect_ContextCanceled(t *testing.T) {
	// Create a server that doesn't respond to handshake
	blockingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block forever, don't complete handshake
		time.Sleep(10 * time.Second)
	}))
	defer blockingServer.Close()

	url := "ws" + strings.TrimPrefix(blockingServer.URL, "http")
	wm := NewWebSocketManager(url, "test-key")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := wm.Connect(ctx)
	if err == nil {
		t.Error("Expected error due to context timeout")
	}

	if wm.IsConnected() {
		t.Error("Should not be connected after context cancellation")
	}
}

func TestWebSocketManager_SendReceive(t *testing.T) {
	// Create echo server
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			err = conn.WriteMessage(messageType, data)
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	// Connect
	wm := NewWebSocketManager(server.URL(), "test-key")
	err := wm.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer wm.Close()

	// Send message
	sendMsg := map[string]string{"test": "hello"}
	err = wm.Send(sendMsg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Receive message
	var recvMsg map[string]string
	err = wm.Receive(context.Background(), &recvMsg)
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	if recvMsg["test"] != "hello" {
		t.Errorf("Expected received message 'hello', got '%s'", recvMsg["test"])
	}
}

func TestWebSocketManager_Send_NotConnected(t *testing.T) {
	wm := NewWebSocketManager("ws://test.example.com", "test-key")

	err := wm.Send(map[string]string{"test": "hello"})
	if err == nil {
		t.Error("Expected error when sending while not connected")
	}
}

func TestWebSocketManager_Send_Closed(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	wm := NewWebSocketManager(server.URL(), "test-key")
	_ = wm.Connect(context.Background())
	_ = wm.Close()

	err := wm.Send(map[string]string{"test": "hello"})
	if err == nil {
		t.Error("Expected error when sending after close")
	}
}

func TestWebSocketManager_Receive_InvalidJSON(t *testing.T) {
	// Server sends invalid JSON
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("invalid json"))
		_, _, _ = conn.ReadMessage() // Keep alive
	})
	defer server.Close()

	wm := NewWebSocketManager(server.URL(), "test-key")
	_ = wm.Connect(context.Background())
	defer wm.Close()

	var msg map[string]string
	err := wm.Receive(context.Background(), &msg)
	if err == nil {
		t.Error("Expected error when receiving invalid JSON")
	}

	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("Expected unmarshal error, got: %v", err)
	}
}

func TestWebSocketManager_Receive_BinaryMessage(t *testing.T) {
	// Server sends binary message
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03})
		_, _, _ = conn.ReadMessage() // Keep alive
	})
	defer server.Close()

	wm := NewWebSocketManager(server.URL(), "test-key")
	_ = wm.Connect(context.Background())
	defer wm.Close()

	var msg map[string]string
	err := wm.Receive(context.Background(), &msg)
	if err == nil {
		t.Error("Expected error when receiving non-JSON binary message")
	}

	// Binary messages are accepted, but non-JSON content will fail to unmarshal
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("Expected unmarshal error, got: %v", err)
	}
}

func TestWebSocketManager_SendPing(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Need to set read deadline and keep reading
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wm := NewWebSocketManager(server.URL(), "test-key")
	_ = wm.Connect(context.Background())
	defer wm.Close()

	err := wm.SendPing()
	if err != nil {
		t.Fatalf("SendPing failed: %v", err)
	}
}

func TestWebSocketManager_StartHeartbeat(t *testing.T) {
	var pingCount atomic.Int32
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		conn.SetPingHandler(func(string) error {
			pingCount.Add(1)
			return nil
		})
		// Keep reading
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wm := NewWebSocketManager(server.URL(), "test-key")
	_ = wm.Connect(context.Background())
	defer wm.Close()

	// Start heartbeat with short interval
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wm.StartHeartbeat(ctx, 50*time.Millisecond)

	// Wait for multiple pings
	time.Sleep(250 * time.Millisecond)

	count := pingCount.Load()
	if count < 3 {
		t.Errorf("Expected at least 3 pings, got %d", count)
	}
}

func TestWebSocketManager_ConnectWithRetry(t *testing.T) {
	attempts := 0
	var mu sync.Mutex

	// Create a server that fails the first 2 connections
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		currentAttempt := attempts
		mu.Unlock()

		if currentAttempt < 3 {
			// Reject connection
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		// Accept connection on 3rd+ attempt
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	wm := NewWebSocketManager(url, "test-key")

	ctx := context.Background()
	err := wm.ConnectWithRetry(ctx)

	if err != nil {
		t.Fatalf("ConnectWithRetry failed: %v", err)
	}

	mu.Lock()
	finalAttempts := attempts
	mu.Unlock()

	if finalAttempts < 3 {
		t.Errorf("Expected at least 3 attempts, got %d", finalAttempts)
	}

	_ = wm.Close()
}

func TestWebSocketManager_ConnectWithRetry_AllFail(t *testing.T) {
	attempts := 0
	var mu sync.Mutex

	// Create a server that always rejects
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	wm := NewWebSocketManager(url, "test-key")

	ctx := context.Background()
	err := wm.ConnectWithRetry(ctx)

	if err == nil {
		t.Error("Expected error after all retry attempts failed")
	}

	_ = wm.Close()
}

func TestWebSocketManager_Close(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	wm := NewWebSocketManager(server.URL(), "test-key")
	_ = wm.Connect(context.Background())

	err := wm.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	if wm.IsConnected() {
		t.Error("Expected not connected after Close()")
	}

	// Test idempotent close
	err = wm.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestWebSocketManager_FullLifecycle(t *testing.T) {
	// Create server that tracks lifecycle
	var lifecycleEvents []string
	var mu sync.Mutex

	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		mu.Lock()
		lifecycleEvents = append(lifecycleEvents, "connected")
		mu.Unlock()

		// Echo messages
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				mu.Lock()
				lifecycleEvents = append(lifecycleEvents, "disconnected")
				mu.Unlock()
				return
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg["type"] == "close" {
				mu.Lock()
				lifecycleEvents = append(lifecycleEvents, "close_requested")
				mu.Unlock()
				return
			}

			_ = conn.WriteMessage(messageType, data)
		}
	})
	defer server.Close()

	// Connect
	wm := NewWebSocketManager(server.URL(), "test-key")
	ctx := context.Background()

	err := wm.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Give time for connection event
	time.Sleep(50 * time.Millisecond)

	// Send/receive
	_ = wm.Send(map[string]string{"type": "ping"})
	var response map[string]string
	_ = wm.Receive(ctx, &response)

	// Request close and close connection
	_ = wm.Send(map[string]string{"type": "close"})
	time.Sleep(50 * time.Millisecond) // Give time for server to process

	_ = wm.Close()

	// Verify lifecycle
	mu.Lock()
	events := make([]string, len(lifecycleEvents))
	copy(events, lifecycleEvents)
	mu.Unlock()

	if len(events) < 2 {
		t.Errorf("Expected at least 2 lifecycle events (connected, close_requested/disconnected), got %d: %v",
			len(events), events)
	}

	// Check that connected was first event
	if len(events) > 0 && events[0] != "connected" {
		t.Errorf("Expected first event to be 'connected', got '%s'", events[0])
	}
}

func TestWebSocketManager_Reset(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	wm := NewWebSocketManager(server.URL(), "test-key")
	_ = wm.Connect(context.Background())

	// Reset should allow reconnection
	wm.Reset()

	// Should be able to connect again
	err := wm.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect after Reset failed: %v", err)
	}
	defer wm.Close()
}

func TestWebSocketManager_SendPing_NotConnected(t *testing.T) {
	wm := NewWebSocketManager("ws://test.example.com", "test-key")
	_ = wm.Close() // Close immediately so IsClosed returns true

	err := wm.SendPing()
	if err == nil {
		t.Error("Expected error when sending ping while not connected")
	}
}

func TestWebSocketManager_Conn(t *testing.T) {
	wm := NewWebSocketManager("ws://test.example.com", "test-key")
	if wm.Conn() == nil {
		t.Error("Expected non-nil underlying Conn")
	}
}
