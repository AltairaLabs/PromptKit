package gemini

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/gorilla/websocket"
)

func TestNewGeminiStreamSession(t *testing.T) {
	// Create mock server
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}
	defer session.Close()

	if session == nil {
		t.Fatal("Expected session to be created")
	}

	if session.ws == nil {
		t.Error("Expected WebSocket manager to be initialized")
	}

	if !session.ws.IsConnected() {
		t.Error("Expected WebSocket to be connected")
	}
}

func TestGeminiStreamSession_SendChunk(t *testing.T) {
	// Create echo server
	var receivedMsg atomic.Bool
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// First read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Now read the actual message
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}

		if msg["client_content"] != nil {
			receivedMsg.Store(true)
		}

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}
	defer session.Close()

	// Send chunk
	chunk := &types.MediaChunk{
		Data:        []byte("audio data"),
		SequenceNum: 1,
		Timestamp:   time.Now(),
	}

	err = session.SendChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("SendChunk failed: %v", err)
	}

	// Give time for message to be received
	time.Sleep(100 * time.Millisecond)

	if !receivedMsg.Load() {
		t.Error("Server did not receive message")
	}
}

func TestGeminiStreamSession_SendText(t *testing.T) {
	var receivedText atomic.Value // stores string
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// First read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Now read the text message
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}

		if clientContent, ok := msg["client_content"].(map[string]interface{}); ok {
			if turns, ok := clientContent["turns"].([]interface{}); ok && len(turns) > 0 {
				if turn, ok := turns[0].(map[string]interface{}); ok {
					if parts, ok := turn["parts"].([]interface{}); ok && len(parts) > 0 {
						if part, ok := parts[0].(map[string]interface{}); ok {
							if text, ok := part["text"].(string); ok {
								receivedText.Store(text)
							}
						}
					}
				}
			}
		}

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}
	defer session.Close()

	// Send text
	err = session.SendText(ctx, "Hello, Gemini!")
	if err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	// Give time for message to be received
	time.Sleep(100 * time.Millisecond)

	received := receivedText.Load()
	if received == nil {
		t.Error("Server did not receive text")
	} else if received.(string) != "Hello, Gemini!" {
		t.Errorf("Expected text 'Hello, Gemini!', got '%s'", received)
	}
}

func TestGeminiStreamSession_CompleteTurn(t *testing.T) {
	var turnComplete atomic.Bool
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// First read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Now read the turn_complete message
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}

		if clientContent, ok := msg["client_content"].(map[string]interface{}); ok {
			if complete, ok := clientContent["turn_complete"].(bool); ok && complete {
				turnComplete.Store(true)
			}
		}

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}
	defer session.Close()

	// Complete turn
	err = session.CompleteTurn(ctx)
	if err != nil {
		t.Fatalf("CompleteTurn failed: %v", err)
	}

	// Give time for message to be received
	time.Sleep(100 * time.Millisecond)

	if !turnComplete.Load() {
		t.Error("Server did not receive turn_complete message")
	}
}

func TestGeminiStreamSession_ReceiveResponse(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// First read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Send a model response
		response := ServerMessage{
			ServerContent: &ServerContent{
				ModelTurn: &ModelTurn{
					Parts: []Part{
						{Text: "Hello from Gemini!"},
					},
				},
				TurnComplete: true,
			},
		}

		data, _ := json.Marshal(response)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}
	defer session.Close()

	// Receive response
	select {
	case chunk := <-session.Response():
		if chunk.Content != "Hello from Gemini!" {
			t.Errorf("Expected 'Hello from Gemini!', got '%s'", chunk.Content)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestGeminiStreamSession_Close(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Keep alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}

	err = session.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Verify session is closed
	if !session.closed {
		t.Error("Expected session to be marked as closed")
	}

	// Verify SendChunk returns error
	chunk := &types.MediaChunk{Data: []byte("test")}
	err = session.SendChunk(ctx, chunk)
	if err == nil {
		t.Error("Expected error when sending chunk after close")
	}

	// Idempotent close
	err = session.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestGeminiStreamSession_Done(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Keep alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}

	// Verify Done is not closed initially
	select {
	case <-session.Done():
		t.Error("Done channel should not be closed initially")
	default:
		// Expected
	}

	// Close session
	session.Close()

	// Verify Done is closed
	select {
	case <-session.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after Close()")
	}
}

func TestGeminiStreamSession_Error(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message then close connection immediately to cause error
		_, _, _ = conn.ReadMessage()
		conn.Close()
	})
	defer server.Close()

	ctx := context.Background()
	_, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	// Expect failure since server closes without sending setup_complete
	if err == nil {
		t.Fatal("Expected error when connection closes during setup")
	}
}

func TestGeminiStreamSession_ContextCancellation(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Keep connection alive
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}
	defer session.Close()

	// Cancel context
	cancel()

	// Verify Done channel is closed
	select {
	case <-session.Done():
		// Expected
	case <-time.After(200 * time.Millisecond):
		t.Error("Done channel should be closed after context cancellation")
	}
}

func TestGeminiStreamSession_MultipleResponses(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Send multiple responses
		responses := []string{"Hello", "from", "Gemini!"}
		for _, text := range responses {
			response := ServerMessage{
				ServerContent: &ServerContent{
					ModelTurn: &ModelTurn{
						Parts: []Part{{Text: text}},
					},
				},
			}
			data, _ := json.Marshal(response)
			_ = conn.WriteMessage(websocket.TextMessage, data)
			time.Sleep(50 * time.Millisecond)
		}

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewGeminiStreamSession(ctx, server.URL(), "test-key")
	if err != nil {
		t.Fatalf("NewGeminiStreamSession failed: %v", err)
	}
	defer session.Close()

	// Receive multiple responses
	expected := []string{"Hello", "from", "Gemini!"}
	received := []string{}

	for i := 0; i < 3; i++ {
		select {
		case chunk := <-session.Response():
			received = append(received, chunk.Content)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timeout waiting for response")
		}
	}

	for i, exp := range expected {
		if i >= len(received) || received[i] != exp {
			t.Errorf("Response %d: expected '%s', got '%s'", i, exp, received[i])
		}
	}
}

func TestBuildClientMessage(t *testing.T) {
	chunk := types.MediaChunk{
		Data:        []byte("audio data"),
		SequenceNum: 1,
		Timestamp:   time.Now(),
	}

	msg := buildClientMessage(chunk, false)

	if msg == nil {
		t.Fatal("Expected message to be built")
	}

	clientContent, ok := msg["client_content"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected client_content field")
	}

	if turnComplete, ok := clientContent["turn_complete"].(bool); !ok || turnComplete {
		t.Error("Expected turn_complete to be false")
	}
}

func TestBuildTextMessage(t *testing.T) {
	text := "Hello, Gemini!"
	msg := buildTextMessage(text, true)

	if msg == nil {
		t.Fatal("Expected message to be built")
	}

	clientContent, ok := msg["client_content"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected client_content field")
	}

	if turnComplete, ok := clientContent["turn_complete"].(bool); !ok || !turnComplete {
		t.Error("Expected turn_complete to be true")
	}

	turns, ok := clientContent["turns"].([]map[string]interface{})
	if !ok || len(turns) == 0 {
		t.Fatal("Expected turns array")
	}

	parts, ok := turns[0]["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		t.Fatal("Expected parts array")
	}

	part, ok := parts[0].(map[string]interface{})
	if !ok {
		t.Fatal("Expected part object")
	}

	if partText, ok := part["text"].(string); !ok || partText != text {
		t.Errorf("Expected text '%s', got '%s'", text, partText)
	}
}
