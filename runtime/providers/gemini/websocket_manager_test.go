package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

	if wm.maxReconnectAttempts != 5 {
		t.Errorf("Expected maxReconnectAttempts 5, got %d", wm.maxReconnectAttempts)
	}

	if wm.baseDelay != 1*time.Second {
		t.Errorf("Expected baseDelay 1s, got %v", wm.baseDelay)
	}

	if wm.IsConnected() {
		t.Error("Expected not connected initially")
	}
}

func TestWebSocketManager_Connect(t *testing.T) {
	// Create mock server
	connected := false
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		connected = true
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

	if !connected {
		t.Error("Server handler was not called")
	}

	// Test idempotent connect
	err = wm.Connect(ctx)
	if err != nil {
		t.Errorf("Second Connect failed: %v", err)
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

	if !strings.Contains(err.Error(), ErrNotConnected) {
		t.Errorf("Expected error to contain '%s', got: %v", ErrNotConnected, err)
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

	if !strings.Contains(err.Error(), ErrManagerClosed) {
		t.Errorf("Expected error to contain '%s', got: %v", ErrManagerClosed, err)
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
		t.Error("Expected error when receiving binary message")
	}

	if !strings.Contains(err.Error(), "unexpected message type") {
		t.Errorf("Expected message type error, got: %v", err)
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

	// Pongs are received automatically by gorilla/websocket's control handler
	// The test verifies no error was returned, which means ping was sent successfully
}

func TestWebSocketManager_StartHeartbeat(t *testing.T) {
	pingCount := 0
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		conn.SetPingHandler(func(string) error {
			pingCount++
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

	if pingCount < 3 {
		t.Errorf("Expected at least 3 pings, got %d", pingCount)
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
	wm.baseDelay = 10 * time.Millisecond // Speed up test
	wm.maxReconnectAttempts = 5

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
	wm.baseDelay = 10 * time.Millisecond
	wm.maxReconnectAttempts = 3

	ctx := context.Background()
	err := wm.ConnectWithRetry(ctx)

	if err == nil {
		t.Error("Expected error after all retry attempts failed")
	}

	mu.Lock()
	finalAttempts := attempts
	mu.Unlock()

	if finalAttempts != 3 {
		t.Errorf("Expected exactly 3 attempts, got %d", finalAttempts)
	}
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

func TestWebSocketManager_calculateBackoff(t *testing.T) {
	wm := NewWebSocketManager("ws://test", "key")
	wm.baseDelay = 1 * time.Second
	wm.maxDelay = 10 * time.Second

	tests := []struct {
		attempt     int
		minExpected time.Duration
		maxExpected time.Duration
	}{
		{0, 750 * time.Millisecond, 1250 * time.Millisecond},  // 1s ±25%
		{1, 1500 * time.Millisecond, 2500 * time.Millisecond}, // 2s ±25%
		{2, 3000 * time.Millisecond, 5000 * time.Millisecond}, // 4s ±25%
		{3, 6000 * time.Millisecond, 10 * time.Second},        // 8s ±25% (capped at 10s)
		{4, 7500 * time.Millisecond, 10 * time.Second},        // 16s capped to 10s, then ±25%
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := wm.calculateBackoff(tt.attempt)

			if delay < tt.minExpected || delay > tt.maxExpected {
				t.Errorf("Attempt %d: expected backoff between %v and %v, got %v",
					tt.attempt, tt.minExpected, tt.maxExpected, delay)
			}
		})
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"401 authentication", fmt.Errorf("401 unauthorized"), false},
		{"400 bad request", fmt.Errorf("400 bad request"), false},
		{"policy violation", fmt.Errorf("policy violation"), false},
		{"1008 policy error", fmt.Errorf("websocket: close 1008"), false},
		{"network error", fmt.Errorf("network timeout"), true},
		{"503 service unavailable", fmt.Errorf("503 service unavailable"), true},
		{"connection refused", fmt.Errorf("connection refused"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldRetry(tt.err)
			if result != tt.expected {
				t.Errorf("shouldRetry(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
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
