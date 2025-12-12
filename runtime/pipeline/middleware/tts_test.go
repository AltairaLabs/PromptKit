package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Mock TTS service for testing
type mockTTSService struct {
	audio    []byte
	mimeType string
	err      error
	callCount int
}

func (m *mockTTSService) Synthesize(ctx context.Context, text string) ([]byte, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	// Return mock audio data (just text prefixed with "audio:")
	return append([]byte("audio:"), []byte(text)...), nil
}

func (m *mockTTSService) MIMEType() string {
	if m.mimeType != "" {
		return m.mimeType
	}
	return types.MIMETypeAudioWAV
}

func TestTTSMiddleware_AddsAudioToChunks(t *testing.T) {
	tts := &mockTTSService{}
	middleware := NewTTSMiddleware(tts, DefaultTTSConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	chunk := &providers.StreamChunk{
		Delta: "Hello world",
	}

	err := middleware.StreamChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("StreamChunk failed: %v", err)
	}

	// Verify audio was added
	if chunk.MediaDelta == nil {
		t.Fatal("Expected MediaDelta to be set")
	}

	if chunk.MediaDelta.Data == nil {
		t.Fatal("Expected MediaDelta.Data to be set")
	}

	expectedAudio := "audio:Hello world"
	if *chunk.MediaDelta.Data != expectedAudio {
		t.Errorf("Expected audio '%s', got '%s'", expectedAudio, *chunk.MediaDelta.Data)
	}

	if chunk.MediaDelta.MIMEType != types.MIMETypeAudioWAV {
		t.Errorf("Expected MIME type '%s', got '%s'", types.MIMETypeAudioWAV, chunk.MediaDelta.MIMEType)
	}

	if tts.callCount != 1 {
		t.Errorf("Expected 1 TTS call, got %d", tts.callCount)
	}
}

func TestTTSMiddleware_ProcessIsNoOp(t *testing.T) {
	tts := &mockTTSService{}
	middleware := NewTTSMiddleware(tts, DefaultTTSConfig())

	ctx := &pipeline.ExecutionContext{
		Context: context.Background(),
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := middleware.Process(ctx, next)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !nextCalled {
		t.Error("next() should be called immediately")
	}

	if tts.callCount != 0 {
		t.Errorf("TTS should not be called during Process, got %d calls", tts.callCount)
	}
}

func TestTTSMiddleware_SkipsNonStreaming(t *testing.T) {
	tts := &mockTTSService{}
	middleware := NewTTSMiddleware(tts, DefaultTTSConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: false, // Not streaming
	}

	chunk := &providers.StreamChunk{
		Delta: "Test text",
	}

	err := middleware.StreamChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("StreamChunk failed: %v", err)
	}

	// Should not add audio
	if chunk.MediaDelta != nil {
		t.Error("MediaDelta should be nil for non-streaming")
	}

	if tts.callCount != 0 {
		t.Errorf("TTS should not be called for non-streaming, got %d calls", tts.callCount)
	}
}

func TestTTSMiddleware_SkipsEmptyChunks(t *testing.T) {
	tts := &mockTTSService{}
	config := TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 1,
	}
	middleware := NewTTSMiddleware(tts, config)

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	testCases := []struct {
		name  string
		chunk providers.StreamChunk
	}{
		{"empty delta", providers.StreamChunk{Delta: ""}},
		{"whitespace only", providers.StreamChunk{Delta: "   "}},
		{"empty content", providers.StreamChunk{Content: ""}},
		{"no text fields", providers.StreamChunk{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tts.callCount = 0
			chunk := tc.chunk

			err := middleware.StreamChunk(ctx, &chunk)
			if err != nil {
				t.Fatalf("StreamChunk failed: %v", err)
			}

			if chunk.MediaDelta != nil {
				t.Error("MediaDelta should be nil for empty chunks")
			}

			if tts.callCount != 0 {
				t.Errorf("TTS should not be called for empty chunks, got %d calls", tts.callCount)
			}
		})
	}
}

func TestTTSMiddleware_MinTextLength(t *testing.T) {
	tts := &mockTTSService{}
	config := TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 5, // Require at least 5 characters
	}
	middleware := NewTTSMiddleware(tts, config)

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	// Test below minimum
	chunk := &providers.StreamChunk{
		Delta: "Hi", // Only 2 characters
	}

	err := middleware.StreamChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("StreamChunk failed: %v", err)
	}

	if chunk.MediaDelta != nil {
		t.Error("MediaDelta should be nil for text below minimum")
	}

	if tts.callCount != 0 {
		t.Errorf("TTS should not be called for short text, got %d calls", tts.callCount)
	}

	// Test at minimum
	chunk2 := &providers.StreamChunk{
		Delta: "Hello", // Exactly 5 characters
	}

	err = middleware.StreamChunk(ctx, chunk2)
	if err != nil {
		t.Fatalf("StreamChunk failed: %v", err)
	}

	if chunk2.MediaDelta == nil {
		t.Error("MediaDelta should be set for text at minimum length")
	}

	if tts.callCount != 1 {
		t.Errorf("TTS should be called once, got %d calls", tts.callCount)
	}
}

func TestTTSMiddleware_TTSError(t *testing.T) {
	tts := &mockTTSService{
		err: errors.New("synthesis failed"),
	}
	middleware := NewTTSMiddleware(tts, DefaultTTSConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	chunk := &providers.StreamChunk{
		Delta: "Test text",
	}

	err := middleware.StreamChunk(ctx, chunk)
	if err == nil {
		t.Fatal("Expected error from TTS failure")
	}

	if err.Error() != "synthesis failed" {
		t.Errorf("Expected 'synthesis failed', got '%v'", err)
	}

	// Verify stream was interrupted
	if !ctx.StreamInterrupted {
		t.Error("Stream should be interrupted on TTS error")
	}

	if !strings.Contains(ctx.InterruptReason, "TTS synthesis failed") {
		t.Errorf("Expected interrupt reason to mention TTS, got '%s'", ctx.InterruptReason)
	}
}

func TestTTSMiddleware_UsesContentField(t *testing.T) {
	tts := &mockTTSService{}
	middleware := NewTTSMiddleware(tts, DefaultTTSConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	chunk := &providers.StreamChunk{
		Content: "Full content text",
	}

	err := middleware.StreamChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("StreamChunk failed: %v", err)
	}

	if chunk.MediaDelta == nil {
		t.Fatal("Expected MediaDelta to be set")
	}

	expectedAudio := "audio:Full content text"
	if *chunk.MediaDelta.Data != expectedAudio {
		t.Errorf("Expected audio '%s', got '%s'", expectedAudio, *chunk.MediaDelta.Data)
	}
}

func TestTTSMiddleware_PrefersDelataOverContent(t *testing.T) {
	tts := &mockTTSService{}
	middleware := NewTTSMiddleware(tts, DefaultTTSConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	chunk := &providers.StreamChunk{
		Delta:   "Delta text",
		Content: "Content text",
	}

	err := middleware.StreamChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("StreamChunk failed: %v", err)
	}

	if chunk.MediaDelta == nil {
		t.Fatal("Expected MediaDelta to be set")
	}

	// Should use Delta, not Content
	expectedAudio := "audio:Delta text"
	if *chunk.MediaDelta.Data != expectedAudio {
		t.Errorf("Expected audio from Delta '%s', got '%s'", expectedAudio, *chunk.MediaDelta.Data)
	}
}

func TestTTSMiddleware_CustomMIMEType(t *testing.T) {
	tts := &mockTTSService{
		mimeType: types.MIMETypeAudioMP3,
	}
	middleware := NewTTSMiddleware(tts, DefaultTTSConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	chunk := &providers.StreamChunk{
		Delta: "Test",
	}

	err := middleware.StreamChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("StreamChunk failed: %v", err)
	}

	if chunk.MediaDelta.MIMEType != types.MIMETypeAudioMP3 {
		t.Errorf("Expected MIME type '%s', got '%s'", types.MIMETypeAudioMP3, chunk.MediaDelta.MIMEType)
	}
}

func TestDefaultTTSConfig(t *testing.T) {
	config := DefaultTTSConfig()

	if !config.SkipEmpty {
		t.Error("Expected SkipEmpty to be true")
	}

	if config.MinTextLength != 1 {
		t.Errorf("Expected MinTextLength 1, got %d", config.MinTextLength)
	}
}
