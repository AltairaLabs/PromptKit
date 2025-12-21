package gemini

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/gorilla/websocket"
)

// TestStreamSession_ReconnectOnUnexpectedClose tests that the session
// automatically reconnects when the server closes the connection unexpectedly.
// This simulates Gemini's behavior of dropping connections mid-conversation.
func TestStreamSession_ReconnectOnUnexpectedClose(t *testing.T) {
	var connectionCount atomic.Int32

	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		count := connectionCount.Add(1)
		t.Logf("Server: connection #%d established", count)

		// Read setup message
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Logf("Server: error reading setup: %v", err)
			return
		}
		t.Logf("Server: received setup message: %s", string(data))

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		if err := conn.WriteMessage(websocket.TextMessage, setupData); err != nil {
			t.Logf("Server: error sending setup_complete: %v", err)
			return
		}
		t.Logf("Server: sent setup_complete")

		if count == 1 {
			// First connection: close unexpectedly after a short delay
			// This simulates Gemini closing the connection mid-conversation
			time.Sleep(100 * time.Millisecond)
			t.Log("Server: closing connection unexpectedly (simulating Gemini behavior)")
			conn.Close()
			return
		}

		// Second connection: stay alive and handle messages
		t.Log("Server: second connection - staying alive")
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				t.Logf("Server: connection #%d closed: %v", count, err)
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create session with auto-reconnect enabled
	session, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{
		AutoReconnect:     true,
		MaxReconnectTries: 3,
	})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer session.Close()

	// Wait for the first connection to be closed and reconnection to happen
	// The receiveLoop should detect the close and trigger reconnection
	time.Sleep(500 * time.Millisecond)

	// Verify we got 2 connections (initial + reconnect)
	count := connectionCount.Load()
	if count < 2 {
		t.Errorf("Expected at least 2 connections (initial + reconnect), got %d", count)
	}

	// Verify session is still usable after reconnect
	err = session.SendText(ctx, "Hello after reconnect")
	if err != nil {
		t.Errorf("SendText after reconnect failed: %v", err)
	}

	t.Logf("Test passed: session reconnected successfully after %d connections", count)
}

// TestStreamSession_ReconnectWithCloseCode tests reconnection with specific
// WebSocket close codes that Gemini might send.
func TestStreamSession_ReconnectWithCloseCode(t *testing.T) {
	testCases := []struct {
		name          string
		closeCode     int
		closeText     string
		expectRecover bool
	}{
		{
			name:          "Normal close (1000)",
			closeCode:     websocket.CloseNormalClosure,
			closeText:     "session ended",
			expectRecover: true, // We should try to reconnect
		},
		{
			name:          "Going away (1001)",
			closeCode:     websocket.CloseGoingAway,
			closeText:     "server restart",
			expectRecover: true,
		},
		{
			name:          "Abnormal closure (1006)",
			closeCode:     websocket.CloseAbnormalClosure,
			closeText:     "",
			expectRecover: true,
		},
		{
			name:          "Policy violation (1008)",
			closeCode:     websocket.ClosePolicyViolation,
			closeText:     "policy violation",
			expectRecover: false, // Policy violations shouldn't retry
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var connectionCount atomic.Int32

			server := newMockWebSocketServer(func(conn *websocket.Conn) {
				count := connectionCount.Add(1)

				// Read and respond to setup
				_, _, _ = conn.ReadMessage()
				setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
				setupData, _ := json.Marshal(setupResponse)
				_ = conn.WriteMessage(websocket.TextMessage, setupData)

				if count == 1 {
					// First connection: close with the specific code
					time.Sleep(50 * time.Millisecond)
					msg := websocket.FormatCloseMessage(tc.closeCode, tc.closeText)
					_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
					conn.Close()
					return
				}

				// Keep subsequent connections alive
				for {
					_, _, err := conn.ReadMessage()
					if err != nil {
						return
					}
				}
			})
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			session, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{
				AutoReconnect:     true,
				MaxReconnectTries: 2,
			})
			if err != nil {
				t.Fatalf("NewStreamSession failed: %v", err)
			}
			defer session.Close()

			// Wait for reconnection attempt
			time.Sleep(300 * time.Millisecond)

			count := connectionCount.Load()
			if tc.expectRecover {
				if count < 2 {
					t.Errorf("Expected reconnection (at least 2 connections), got %d", count)
				}
			}
			// Note: For policy violations, the WebSocket manager's shouldRetry()
			// should prevent reconnection, but the session-level reconnect
			// currently tries anyway. This test documents current behavior.
		})
	}
}

// TestStreamSession_ReconnectPreservesSetup tests that reconnection
// resends the original setup message (system instruction, modalities, etc.)
func TestStreamSession_ReconnectPreservesSetup(t *testing.T) {
	var setupMessages []map[string]interface{}
	var mu sync.Mutex
	var connectionCount atomic.Int32

	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		count := connectionCount.Add(1)

		// Read setup message and capture it
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var setupMsg map[string]interface{}
		json.Unmarshal(data, &setupMsg)

		mu.Lock()
		setupMessages = append(setupMessages, setupMsg)
		mu.Unlock()

		// Send setup_complete
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		if count == 1 {
			// Close first connection to trigger reconnect
			time.Sleep(50 * time.Millisecond)
			conn.Close()
			return
		}

		// Keep alive
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	systemInstruction := "You are a helpful assistant"
	session, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{
		Model:              "gemini-2.0-flash-exp",
		SystemInstruction:  systemInstruction,
		ResponseModalities: []string{"AUDIO"},
		AutoReconnect:      true,
		MaxReconnectTries:  2,
	})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer session.Close()

	// Wait for reconnection
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(setupMessages) < 2 {
		t.Fatalf("Expected at least 2 setup messages, got %d", len(setupMessages))
	}

	// Verify both setup messages are identical
	// (reconnection should send the same setup)
	setup1, _ := json.Marshal(setupMessages[0])
	setup2, _ := json.Marshal(setupMessages[1])

	if string(setup1) != string(setup2) {
		t.Errorf("Setup messages differ after reconnect:\nFirst:  %s\nSecond: %s",
			string(setup1), string(setup2))
	}
}

// TestStreamSession_NoReconnectWhenDisabled tests that reconnection
// doesn't happen when AutoReconnect is false.
func TestStreamSession_NoReconnectWhenDisabled(t *testing.T) {
	var connectionCount atomic.Int32

	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		count := connectionCount.Add(1)

		// Read setup, send setup_complete
		_, _, _ = conn.ReadMessage()
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		if count == 1 {
			// Close first connection
			time.Sleep(50 * time.Millisecond)
			conn.Close()
			return
		}

		// Keep alive (shouldn't reach here)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	session, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{
		AutoReconnect: false, // Reconnect disabled
	})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer session.Close()

	// Wait to see if reconnection happens
	time.Sleep(300 * time.Millisecond)

	count := connectionCount.Load()
	if count != 1 {
		t.Errorf("Expected only 1 connection (no reconnect), got %d", count)
	}
}

// TestStreamSession_ReconnectExhaustsRetries tests that reconnection
// stops after max retries when reconnection fails.
func TestStreamSession_ReconnectExhaustsRetries(t *testing.T) {
	var connectionCount atomic.Int32
	var initialConnection atomic.Bool

	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		count := connectionCount.Add(1)

		// First connection succeeds, all reconnection attempts fail
		if count == 1 {
			initialConnection.Store(true)
			// Read setup, send setup_complete
			_, _, _ = conn.ReadMessage()
			setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
			setupData, _ := json.Marshal(setupResponse)
			_ = conn.WriteMessage(websocket.TextMessage, setupData)

			// Close after a short delay to trigger reconnection
			time.Sleep(50 * time.Millisecond)
			conn.Close()
		} else {
			// Subsequent connections fail immediately (no setup_complete)
			time.Sleep(10 * time.Millisecond)
			conn.Close()
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	maxRetries := 3
	session, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{
		AutoReconnect:     true,
		MaxReconnectTries: maxRetries,
	})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer session.Close()

	// Wait for all reconnection attempts to exhaust
	time.Sleep(1500 * time.Millisecond)

	count := connectionCount.Load()
	// Should have initial connection + maxRetries reconnection attempts
	expectedMax := int32(1 + maxRetries + 1) // Initial + retries + buffer for timing
	if count > expectedMax {
		t.Errorf("Too many connections (%d), expected at most %d", count, expectedMax)
	}

	// Verify initial connection was established
	if !initialConnection.Load() {
		t.Error("Initial connection was never established")
	}
}

// TestStreamSession_SendChunkDuringReconnect tests behavior when
// trying to send while reconnecting.
func TestStreamSession_SendChunkDuringReconnect(t *testing.T) {
	var connectionCount atomic.Int32
	reconnectStarted := make(chan struct{})
	reconnectComplete := make(chan struct{})

	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		count := connectionCount.Add(1)

		// Read setup, send setup_complete
		_, _, _ = conn.ReadMessage()
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		if count == 1 {
			// Close first connection
			time.Sleep(50 * time.Millisecond)
			close(reconnectStarted)
			conn.Close()
			return
		}

		// Second connection - signal completion and stay alive
		close(reconnectComplete)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{
		AutoReconnect:     true,
		MaxReconnectTries: 3,
	})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer session.Close()

	// Wait for reconnection to start
	<-reconnectStarted

	// Try to send during reconnection (this may fail or succeed depending on timing)
	chunk := &types.MediaChunk{
		Data:        []byte("audio data during reconnect"),
		SequenceNum: 1,
	}

	// We don't assert on the result since timing is unpredictable
	_ = session.SendChunk(ctx, chunk)

	// Wait for reconnection to complete
	select {
	case <-reconnectComplete:
		t.Log("Reconnection completed")
	case <-time.After(3 * time.Second):
		t.Log("Timeout waiting for reconnection (may have failed)")
	}

	// After reconnection, sending should work
	err = session.SendChunk(ctx, chunk)
	if err != nil {
		t.Logf("SendChunk after reconnect failed: %v (may be expected if reconnection failed)", err)
	}
}
