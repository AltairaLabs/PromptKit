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

	// Audio must use the CURRENT realtimeInput.audio wire format. The legacy
	// realtime_input.media_chunks form is deprecated and closes current Gemini
	// Live models (e.g. gemini-*-live) with websocket close 1007. (#1666)
	if _, legacy := msg["realtime_input"]; legacy {
		t.Fatal("audio must not use the deprecated realtime_input.media_chunks form")
	}

	realtimeInput, ok := msg["realtimeInput"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput in message")
	}

	audio, ok := realtimeInput["audio"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput.audio object")
	}

	// Gemini Live requires the sample rate in the audio mimeType.
	if mimeType, _ := audio["mimeType"].(string); mimeType != "audio/pcm;rate=16000" {
		t.Errorf("Expected mimeType 'audio/pcm;rate=16000', got %v", mimeType)
	}

	if data, _ := audio["data"].(string); data == "" {
		t.Error("Expected base64 encoded data")
	}
}

func TestBuildClientMessage_ImageJPEG(t *testing.T) {
	// Test buildClientMessage with image data - should use TypeScript SDK format
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // Fake JPEG header

	chunk := types.MediaChunk{
		Data:      imageData,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"mime_type": "image/jpeg",
		},
	}

	msg := buildClientMessage(chunk, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	// Image should use camelCase "realtimeInput" format (TypeScript SDK style)
	realtimeInput, ok := msg["realtimeInput"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput (camelCase) in message for images")
	}

	// Should use singular "media" object, not "media_chunks" array
	media, ok := realtimeInput["media"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected media object in realtimeInput")
	}

	mimeType, ok := media["mimeType"].(string)
	if !ok || mimeType != "image/jpeg" {
		t.Errorf("Expected mimeType 'image/jpeg', got %v", mimeType)
	}

	data, ok := media["data"].(string)
	if !ok || data == "" {
		t.Error("Expected base64 encoded data")
	}
}

func TestBuildClientMessage_VideoPNG(t *testing.T) {
	// Test buildClientMessage with video/PNG data
	videoData := []byte{0x89, 0x50, 0x4E, 0x47} // Fake PNG header

	chunk := types.MediaChunk{
		Data:      videoData,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"mime_type": "video/mp4",
		},
	}

	msg := buildClientMessage(chunk, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	// Video should also use camelCase "realtimeInput" format
	realtimeInput, ok := msg["realtimeInput"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput (camelCase) in message for video")
	}

	media, ok := realtimeInput["media"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected media object in realtimeInput")
	}

	mimeType, ok := media["mimeType"].(string)
	if !ok || mimeType != "video/mp4" {
		t.Errorf("Expected mimeType 'video/mp4', got %v", mimeType)
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
