package gemini

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestSendSystemContext_NilSession(t *testing.T) {
	// Test that SendSystemContext handles closed session correctly
	session := &StreamSession{
		closed: true,
	}

	err := session.SendSystemContext(context.Background(), "test")
	if err == nil {
		t.Error("Expected error when sending to closed session")
	}
}

func TestEndInput_SilenceFrames(t *testing.T) {
	// This tests the logic of EndInput without needing a real WebSocket
	// The function should create and send silence frames

	// We can't easily test this without mocking, but we can at least
	// verify the function exists and can be called
	// (coverage will increase from actually calling it)

	// Note: In a real test environment, this would need a mock WebSocket
	// For now, we're documenting that this method needs integration testing
	t.Skip("EndInput requires WebSocket connection - needs integration test")
}

func TestBuildTextMessage_TurnComplete(t *testing.T) {
	// Test buildTextMessage with turnComplete flag
	text := "Hello"

	// Test with turn_complete = false
	msg := buildTextMessage(text, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	clientContent, ok := msg["client_content"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected client_content in message")
	}

	turnComplete, ok := clientContent["turn_complete"].(bool)
	if !ok {
		t.Fatal("Expected turn_complete field")
	}

	if turnComplete {
		t.Error("Expected turn_complete to be false")
	}

	// Test with turn_complete = true
	msg2 := buildTextMessage(text, true)
	clientContent2, _ := msg2["client_content"].(map[string]interface{})
	turnComplete2, _ := clientContent2["turn_complete"].(bool)

	if !turnComplete2 {
		t.Error("Expected turn_complete to be true")
	}
}

func TestBuildClientMessage_AudioPCM(t *testing.T) {
	// Test buildClientMessage with audio data
	audioData := make([]byte, 1000)
	for i := range audioData {
		audioData[i] = byte(i % 256)
	}

	chunk := types.MediaChunk{
		Data:      audioData,
		Timestamp: time.Now(),
	}

	msg := buildClientMessage(chunk, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	realtimeInput, ok := msg["realtime_input"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtime_input in message")
	}

	mediaChunks, ok := realtimeInput["media_chunks"].([]map[string]interface{})
	if !ok || len(mediaChunks) == 0 {
		t.Fatal("Expected media_chunks array")
	}

	firstChunk := mediaChunks[0]
	mimeType, ok := firstChunk["mime_type"].(string)
	if !ok || mimeType != "audio/pcm" {
		t.Errorf("Expected mime_type 'audio/pcm', got %v", mimeType)
	}

	data, ok := firstChunk["data"].(string)
	if !ok || data == "" {
		t.Error("Expected base64 encoded data")
	}
}

func TestCompleteTurn_ClosedSession(t *testing.T) {
	session := &StreamSession{
		closed: true,
	}

	err := session.CompleteTurn(context.Background())
	if err == nil {
		t.Error("Expected error when completing turn on closed session")
	}
}
