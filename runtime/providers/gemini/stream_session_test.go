package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/gorilla/websocket"
)

func TestNewStreamSession(t *testing.T) {
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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

		// Check for realtime_input format (new format for audio chunks)
		if msg["realtime_input"] != nil {
			receivedMsg.Store(true)
		}

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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

func TestGeminiStreamSession_ReceiveAudioResponse(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// First read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Send a model response with audio data
		audioData := "SGVsbG8gV29ybGQ=" // Base64 encoded "Hello World"
		response := ServerMessage{
			ServerContent: &ServerContent{
				ModelTurn: &ModelTurn{
					Parts: []Part{
						{
							InlineData: &InlineData{
								MimeType: "audio/pcm",
								Data:     audioData,
							},
						},
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer session.Close()

	// Receive response with audio
	select {
	case chunk := <-session.Response():
		// Verify MediaDelta is populated
		if chunk.MediaDelta == nil {
			t.Fatal("Expected MediaDelta to be populated")
		}

		if chunk.MediaDelta.MIMEType != "audio/pcm" {
			t.Errorf("Expected MIMEType 'audio/pcm', got '%s'", chunk.MediaDelta.MIMEType)
		}

		if chunk.MediaDelta.Data == nil {
			t.Fatal("Expected Data to be populated")
		}

		if *chunk.MediaDelta.Data != "SGVsbG8gV29ybGQ=" {
			t.Errorf("Expected base64 data 'SGVsbG8gV29ybGQ=', got '%s'", *chunk.MediaDelta.Data)
		}

		// Verify audio metadata is populated
		if chunk.MediaDelta.Channels == nil {
			t.Error("Expected Channels to be populated")
		} else if *chunk.MediaDelta.Channels != 1 {
			t.Errorf("Expected Channels to be 1, got %d", *chunk.MediaDelta.Channels)
		}

		if chunk.MediaDelta.BitRate == nil {
			t.Error("Expected BitRate (sample rate) to be populated")
		} else if *chunk.MediaDelta.BitRate != 16000 {
			t.Errorf("Expected BitRate to be 16000, got %d", *chunk.MediaDelta.BitRate)
		}

		// Verify finish reason
		if chunk.FinishReason == nil {
			t.Error("Expected FinishReason to be set")
		} else if *chunk.FinishReason != "complete" {
			t.Errorf("Expected FinishReason 'complete', got '%s'", *chunk.FinishReason)
		}

	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestGeminiStreamSession_ReceiveMixedResponse(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// First read setup message
		_, _, _ = conn.ReadMessage()

		// Send setup_complete response
		setupResponse := ServerMessage{
			SetupComplete: &SetupComplete{},
		}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)

		// Send a model response with both text and audio
		audioData := "YXVkaW8=" // Base64 encoded "audio"
		response := ServerMessage{
			ServerContent: &ServerContent{
				ModelTurn: &ModelTurn{
					Parts: []Part{
						{Text: "Here is your audio:"},
						{
							InlineData: &InlineData{
								MimeType: "audio/pcm",
								Data:     audioData,
							},
						},
					},
				},
				TurnComplete: false, // Not complete yet
			},
		}

		data, _ := json.Marshal(response)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Keep connection alive
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
	}
	defer session.Close()

	// Receive mixed response
	select {
	case chunk := <-session.Response():
		// Verify both text and audio are present
		if chunk.Content != "Here is your audio:" {
			t.Errorf("Expected text 'Here is your audio:', got '%s'", chunk.Content)
		}

		if chunk.MediaDelta == nil {
			t.Fatal("Expected MediaDelta to be populated")
		}

		if *chunk.MediaDelta.Data != "YXVkaW8=" {
			t.Errorf("Expected audio data 'YXVkaW8=', got '%s'", *chunk.MediaDelta.Data)
		}

		// Verify finish reason is nil (not complete)
		if chunk.FinishReason != nil {
			t.Errorf("Expected FinishReason to be nil, got '%s'", *chunk.FinishReason)
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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
	t.Run("error during setup", func(t *testing.T) {
		server := newMockWebSocketServer(func(conn *websocket.Conn) {
			// Read setup message then close connection immediately to cause error
			_, _, _ = conn.ReadMessage()
			conn.Close()
		})
		defer server.Close()

		ctx := context.Background()
		_, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
		// Expect failure since server closes without sending setup_complete
		if err == nil {
			t.Fatal("Expected error when connection closes during setup")
		}
	})

	t.Run("error during receive loop", func(t *testing.T) {
		server := newMockWebSocketServer(func(conn *websocket.Conn) {
			// Read setup message
			_, _, _ = conn.ReadMessage()

			// Send setup_complete response
			setupResponse := ServerMessage{
				SetupComplete: &SetupComplete{},
			}
			setupData, _ := json.Marshal(setupResponse)
			_ = conn.WriteMessage(websocket.TextMessage, setupData)

			// Send invalid JSON to cause error
			_ = conn.WriteMessage(websocket.TextMessage, []byte("invalid json"))
		})
		defer server.Close()

		ctx := context.Background()
		session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		defer session.Close()

		// Wait for error to propagate
		time.Sleep(50 * time.Millisecond)

		// Check Error() method
		sessionErr := session.Error()
		if sessionErr == nil {
			t.Error("Expected Error() to return an error after invalid JSON")
		}
	})

	t.Run("no error when session is healthy", func(t *testing.T) {
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
			time.Sleep(100 * time.Millisecond)
		})
		defer server.Close()

		ctx := context.Background()
		session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		defer session.Close()

		// Check Error() method immediately - should return nil
		sessionErr := session.Error()
		if sessionErr != nil {
			t.Errorf("Expected Error() to return nil for healthy session, got: %v", sessionErr)
		}
	})
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("NewStreamSession failed: %v", err)
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

	// New format uses realtime_input instead of client_content
	realtimeInput, ok := msg["realtime_input"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtime_input field")
	}

	mediaChunks, ok := realtimeInput["media_chunks"].([]map[string]interface{})
	if !ok || len(mediaChunks) == 0 {
		t.Fatal("Expected media_chunks array")
	}

	if mimeType, ok := mediaChunks[0]["mime_type"].(string); !ok || mimeType != "audio/pcm" {
		t.Error("Expected mime_type to be 'audio/pcm'")
	}

	if data, ok := mediaChunks[0]["data"].(string); !ok || data == "" {
		t.Error("Expected base64 encoded data")
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

// TestNewStreamSession_RejectsTextAndAudioTogether verifies that the session
// rejects configurations that request both TEXT and AUDIO modalities.
// The Gemini Live API does not support this combination.
func TestNewStreamSession_RejectsTextAndAudioTogether(t *testing.T) {
	ctx := context.Background()

	// This should fail BEFORE connecting to any server
	_, err := NewStreamSession(ctx, "wss://example.com", "test-key", StreamSessionConfig{
		Model:              "gemini-2.0-flash-exp",
		ResponseModalities: []string{"TEXT", "AUDIO"},
	})

	if err == nil {
		t.Fatal("Expected error when requesting both TEXT and AUDIO modalities")
	}

	expectedMsg := "does not support TEXT and AUDIO simultaneously"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got: %v", expectedMsg, err)
	}
}

// TestNewStreamSession_AcceptsValidModalities verifies valid modality configurations
func TestNewStreamSession_AcceptsValidModalities(t *testing.T) {
	// Create mock server
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()

	testCases := []struct {
		name       string
		modalities []string
	}{
		{"TEXT only", []string{"TEXT"}},
		{"AUDIO only", []string{"AUDIO"}},
		{"empty (defaults to TEXT)", []string{}},
		{"nil (defaults to TEXT)", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{
				ResponseModalities: tc.modalities,
			})
			if err != nil {
				t.Fatalf("Unexpected error for %s: %v", tc.name, err)
			}
			session.Close()
		})
	}
}
// TestStreamSession_SendAfterClose tests sending after session is closed
func TestStreamSession_SendAfterClose(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)
	})
	defer server.Close()

	ctx := context.Background()
	session, _ := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	
	// Close the session
	session.Close()
	
	// Try to send text after close
	err := session.SendText(ctx, "test")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("Expected closed error, got: %v", err)
	}
	
	// Try to send chunk after close
	chunk := &types.MediaChunk{Data: []byte("test")}
	err = session.SendChunk(ctx, chunk)
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("Expected closed error, got: %v", err)
	}
}

// TestTruncateInlineData tests truncation of large data fields
func TestTruncateInlineData(t *testing.T) {
	// Test with map containing long data
	data := map[string]interface{}{
		"data": strings.Repeat("x", 200),
		"other": "value",
	}
	
	truncateInlineData(data)
	
	dataVal, ok := data["data"].(string)
	if !ok {
		t.Fatal("data field should still be string")
	}
	
	if !strings.Contains(dataVal, "bytes") {
		t.Error("Expected data to be truncated with byte count")
	}
	
	// Test with nested structures
	nested := map[string]interface{}{
		"outer": map[string]interface{}{
			"data": strings.Repeat("y", 150),
		},
		"array": []interface{}{
			map[string]interface{}{
				"data": strings.Repeat("z", 120),
			},
		},
	}
	
	truncateInlineData(nested)
	
	// Verify nested map was truncated
	outer := nested["outer"].(map[string]interface{})
	outerData := outer["data"].(string)
	if !strings.Contains(outerData, "bytes") {
		t.Error("Expected nested data to be truncated")
	}
	
	// Verify array element was truncated
	arrayElems := nested["array"].([]interface{})
	if len(arrayElems) > 0 {
		elem := arrayElems[0].(map[string]interface{})
		elemData := elem["data"].(string)
		if !strings.Contains(elemData, "bytes") {
			t.Error("Expected array element data to be truncated")
		}
	}
}

func TestProcessServerMessage_Interruption(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()
		
		// Send setup_complete
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)
		
		// Send interruption message
		interruptMsg := ServerMessage{
			ServerContent: &ServerContent{
				Interrupted: true,
			},
		}
		msgData, _ := json.Marshal(interruptMsg)
		_ = conn.WriteMessage(websocket.TextMessage, msgData)
		
		time.Sleep(100 * time.Millisecond)
	})
	defer server.Close()
	
	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()
	
	// Wait for interruption message
	select {
	case chunk := <-session.responseCh:
		if !chunk.Interrupted {
			t.Error("Expected interrupted flag to be true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for interruption")
	}
}

func TestProcessServerMessage_InputTranscription(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()
		
		// Send setup_complete
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)
		
		// Send input transcription
		transcriptMsg := ServerMessage{
			ServerContent: &ServerContent{
				InputTranscription: &Transcription{
					Text: "Hello world",
				},
				TurnComplete: false,
			},
		}
		msgData, _ := json.Marshal(transcriptMsg)
		_ = conn.WriteMessage(websocket.TextMessage, msgData)
		
		time.Sleep(100 * time.Millisecond)
	})
	defer server.Close()
	
	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()
	
	// Wait for transcription
	select {
	case chunk := <-session.responseCh:
		if chunk.Metadata == nil {
			t.Fatal("Expected metadata")
		}
		if chunk.Metadata["type"] != "input_transcription" {
			t.Errorf("Expected type=input_transcription, got %v", chunk.Metadata["type"])
		}
		if chunk.Metadata["transcription"] != "Hello world" {
			t.Errorf("Expected transcription='Hello world', got %v", chunk.Metadata["transcription"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for input transcription")
	}
}

func TestProcessServerMessage_OutputTranscription(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()
		
		// Send setup_complete
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)
		
		// Send output transcription
		transcriptMsg := ServerMessage{
			ServerContent: &ServerContent{
				OutputTranscription: &Transcription{
					Text: "Model response",
				},
				TurnComplete: false,
			},
		}
		msgData, _ := json.Marshal(transcriptMsg)
		_ = conn.WriteMessage(websocket.TextMessage, msgData)
		
		time.Sleep(100 * time.Millisecond)
	})
	defer server.Close()
	
	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()
	
	// Wait for transcription
	select {
	case chunk := <-session.responseCh:
		if chunk.Delta != "Model response" {
			t.Errorf("Expected delta='Model response', got %v", chunk.Delta)
		}
		if chunk.Metadata == nil {
			t.Fatal("Expected metadata")
		}
		if chunk.Metadata["type"] != "output_transcription" {
			t.Errorf("Expected type=output_transcription, got %v", chunk.Metadata["type"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for output transcription")
	}
}

func TestProcessServerMessage_TurnCompleteWithoutModelTurn(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()
		
		// Send setup_complete
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)
		
		// Send turn complete without model turn
		completeMsg := ServerMessage{
			ServerContent: &ServerContent{
				TurnComplete: true,
				ModelTurn:    nil,
			},
		}
		msgData, _ := json.Marshal(completeMsg)
		_ = conn.WriteMessage(websocket.TextMessage, msgData)
		
		time.Sleep(100 * time.Millisecond)
	})
	defer server.Close()
	
	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()
	
	// Wait for completion
	select {
	case chunk := <-session.responseCh:
		if chunk.FinishReason == nil {
			t.Fatal("Expected finish reason")
		}
		if *chunk.FinishReason != "complete" {
			t.Errorf("Expected finish_reason='complete', got %v", *chunk.FinishReason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for turn complete")
	}
}

func TestProcessServerMessage_ToolCall(t *testing.T) {
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		// Read setup message
		_, _, _ = conn.ReadMessage()
		
		// Send setup_complete
		setupResponse := ServerMessage{SetupComplete: &SetupComplete{}}
		setupData, _ := json.Marshal(setupResponse)
		_ = conn.WriteMessage(websocket.TextMessage, setupData)
		
		// Send tool call message
		toolCallMsg := ServerMessage{
			ToolCall: &ToolCallMsg{
				FunctionCalls: []FunctionCall{
					{
						ID:   "call_1",
						Name: "test_function",
						Args: map[string]interface{}{"arg": "value"},
					},
				},
			},
		}
		msgData, _ := json.Marshal(toolCallMsg)
		_ = conn.WriteMessage(websocket.TextMessage, msgData)
		
		time.Sleep(100 * time.Millisecond)
	})
	defer server.Close()
	
	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", StreamSessionConfig{})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()
	
	// Tool calls are currently no-op, just verify no error
	time.Sleep(200 * time.Millisecond)
}
