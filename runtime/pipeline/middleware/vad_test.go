package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Mock VAD analyzer for testing
type mockVADAnalyzer struct {
	scores []float64
	index  int
	err    error
}

func (m *mockVADAnalyzer) Analyze(ctx context.Context, audioData []byte) (float64, error) {
	if m.err != nil {
		return 0, m.err
	}
	if m.index >= len(m.scores) {
		return 0.0, nil // Default to silence
	}
	score := m.scores[m.index]
	m.index++
	return score, nil
}

func (m *mockVADAnalyzer) State() audio.VADState {
	if m.index > 0 && m.index <= len(m.scores) && m.scores[m.index-1] >= 0.5 {
		return audio.VADStateSpeaking
	}
	return audio.VADStateQuiet
}

func (m *mockVADAnalyzer) Name() string {
	return "mock-vad"
}

func (m *mockVADAnalyzer) OnStateChange() <-chan audio.VADEvent {
	ch := make(chan audio.VADEvent)
	close(ch)
	return ch
}

func (m *mockVADAnalyzer) Reset() {
	m.index = 0
}

// Mock transcription service for testing
type mockTranscriber struct {
	text string
	err  error
}

func (m *mockTranscriber) Transcribe(ctx context.Context, audio []byte) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.text, nil
}

func TestVADMiddleware_BlocksUntilTurnComplete(t *testing.T) {
	// Setup: VAD scores representing speech (0.8) then silence (0.2)
	vad := &mockVADAnalyzer{
		scores: []float64{0.8, 0.8, 0.8, 0.2, 0.2, 0.2}, // Speech then silence
	}
	transcriber := &mockTranscriber{text: "Hello world"}
	config := VADConfig{
		Threshold:       0.3,
		SilenceDuration: 100 * time.Millisecond,
	}
	middleware := NewVADMiddleware(vad, transcriber, config)

	// Create StreamInput channel and send chunks
	streamInput := make(chan providers.StreamChunk, 10)
	audioChunk := []byte{1, 2, 3, 4}

	// Send chunks in background (simulating user speaking)
	go func() {
		for i := 0; i < 6; i++ {
			audioStr := string(audioChunk)
			streamInput <- providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     &audioStr,
				},
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(streamInput)
	}()

	// Create execution context
	ctx := &pipeline.ExecutionContext{
		Context:     context.Background(),
		StreamMode:  true,
		StreamInput: streamInput,
		Messages:    []types.Message{},
	}

	// Track if next() was called
	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	// Process should block until turn complete, then call next()
	err := middleware.Process(ctx, next)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify next() was called
	if !nextCalled {
		t.Error("next() was not called after turn complete")
	}

	// Verify Message was created
	if len(ctx.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(ctx.Messages))
	}

	msg := ctx.Messages[0]
	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", msg.Role)
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Text == nil || *msg.Parts[0].Text != "Hello world" {
		t.Errorf("Expected text 'Hello world', got %+v", msg.Parts)
	}
}

func TestVADMiddleware_SkipsNonStreaming(t *testing.T) {
	vad := &mockVADAnalyzer{}
	transcriber := &mockTranscriber{text: "Test"}
	middleware := NewVADMiddleware(vad, transcriber, DefaultVADConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: false, // Not streaming
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

	// Should immediately call next() without processing
	if !nextCalled {
		t.Error("next() should be called when not streaming")
	}
}

func TestVADMiddleware_SkipsNoStreamInput(t *testing.T) {
	vad := &mockVADAnalyzer{}
	transcriber := &mockTranscriber{text: "Test"}
	middleware := NewVADMiddleware(vad, transcriber, DefaultVADConfig())

	ctx := &pipeline.ExecutionContext{
		Context:     context.Background(),
		StreamMode:  true,
		StreamInput: nil, // No input channel
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

	// Should immediately call next() without processing
	if !nextCalled {
		t.Error("next() should be called when StreamInput is nil")
	}
}

func TestVADMiddleware_TranscriptionError(t *testing.T) {
	vad := &mockVADAnalyzer{
		scores: []float64{0.8, 0.2, 0.2}, // Speech then silence
	}
	transcriber := &mockTranscriber{
		err: errors.New("transcription failed"),
	}
	config := VADConfig{
		Threshold:       0.3,
		SilenceDuration: 50 * time.Millisecond,
	}
	middleware := NewVADMiddleware(vad, transcriber, config)

	streamInput := make(chan providers.StreamChunk, 10)
	go func() {
		for i := 0; i < 3; i++ {
			streamInput <- providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     func() *string { s := string([]byte{1, 2, 3}); return &s }(),
				},
			}
			time.Sleep(30 * time.Millisecond)
		}
		close(streamInput)
	}()

	ctx := &pipeline.ExecutionContext{
		Context:     context.Background(),
		StreamMode:  true,
		StreamInput: streamInput,
		Messages:    []types.Message{},
	}

	err := middleware.Process(ctx, func() error { return nil })
	if err == nil {
		t.Fatal("Expected error from transcription failure")
	}
	if err.Error() != "transcription failed" {
		t.Errorf("Expected 'transcription failed', got '%v'", err)
	}
}

func TestVADMiddleware_VADError(t *testing.T) {
	vad := &mockVADAnalyzer{
		err: errors.New("VAD analysis failed"),
	}
	transcriber := &mockTranscriber{text: "Test"}
	middleware := NewVADMiddleware(vad, transcriber, DefaultVADConfig())

	streamInput := make(chan providers.StreamChunk, 1)
	streamInput <- providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: types.MIMETypeAudioWAV,
			Data:     func() *string { s := string([]byte{1, 2, 3}); return &s }(),
		},
	}
	close(streamInput)

	ctx := &pipeline.ExecutionContext{
		Context:     context.Background(),
		StreamMode:  true,
		StreamInput: streamInput,
	}

	err := middleware.Process(ctx, func() error { return nil })
	if err == nil {
		t.Fatal("Expected error from VAD failure")
	}
	if err.Error() != "VAD analysis failed" {
		t.Errorf("Expected 'VAD analysis failed', got '%v'", err)
	}
}

func TestVADMiddleware_MaxTurnDuration(t *testing.T) {
	// VAD that always returns speech (never silence)
	vad := &mockVADAnalyzer{
		scores: []float64{0.8, 0.8, 0.8, 0.8, 0.8, 0.8},
	}
	transcriber := &mockTranscriber{text: "Long speech"}
	config := VADConfig{
		Threshold:       0.3,
		MaxTurnDuration: 200 * time.Millisecond, // Force completion quickly
		SilenceDuration: 1 * time.Second,        // High to prevent early completion
	}
	middleware := NewVADMiddleware(vad, transcriber, config)

	streamInput := make(chan providers.StreamChunk, 10)
	go func() {
		for i := 0; i < 10; i++ {
			streamInput <- providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     func() *string { s := string([]byte{1, 2, 3}); return &s }(),
				},
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(streamInput)
	}()

	ctx := &pipeline.ExecutionContext{
		Context:     context.Background(),
		StreamMode:  true,
		StreamInput: streamInput,
		Messages:    []types.Message{},
	}

	nextCalled := false
	err := middleware.Process(ctx, func() error {
		nextCalled = true
		return nil
	})

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !nextCalled {
		t.Error("next() should be called after MaxTurnDuration")
	}

	if len(ctx.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(ctx.Messages))
	}
}

func TestVADMiddleware_ContextCancellation(t *testing.T) {
	vad := &mockVADAnalyzer{
		scores: []float64{0.8, 0.8, 0.8}, // Always speech
	}
	transcriber := &mockTranscriber{text: "Test"}
	middleware := NewVADMiddleware(vad, transcriber, DefaultVADConfig())

	streamInput := make(chan providers.StreamChunk, 10)
	cancelCtx, cancel := context.WithCancel(context.Background())

	ctx := &pipeline.ExecutionContext{
		Context:     cancelCtx,
		StreamMode:  true,
		StreamInput: streamInput,
	}

	// Cancel context while processing
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Send chunks that won't trigger turn complete
	go func() {
		for i := 0; i < 10; i++ {
			streamInput <- providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     func() *string { s := string([]byte{1, 2, 3}); return &s }(),
				},
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	err := middleware.Process(ctx, func() error { return nil })
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestVADMiddleware_SkipsNonAudioChunks(t *testing.T) {
	vad := &mockVADAnalyzer{
		scores: []float64{0.8, 0.8, 0.2, 0.2}, // Speech then silence
	}
	transcriber := &mockTranscriber{text: "Audio only"}
	config := VADConfig{
		Threshold:         0.3,
		SilenceDuration:   50 * time.Millisecond,
		MaxTurnDuration:   5 * time.Second, // Prevent immediate turn completion
		MinSpeechDuration: 10 * time.Millisecond,
	}
	middleware := NewVADMiddleware(vad, transcriber, config)

	streamInput := make(chan providers.StreamChunk, 10)
	go func() {
		// Send text chunk (should be skipped)
		streamInput <- providers.StreamChunk{
			Delta: "This is text",
		}
		// Send audio chunks
		for i := 0; i < 4; i++ {
			streamInput <- providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     func() *string { s := string([]byte{1, 2, 3}); return &s }(),
				},
			}
			time.Sleep(30 * time.Millisecond)
		}
		close(streamInput)
	}()

	ctx := &pipeline.ExecutionContext{
		Context:     context.Background(),
		StreamMode:  true,
		StreamInput: streamInput,
		Messages:    []types.Message{},
	}

	nextCalled := false
	err := middleware.Process(ctx, func() error {
		nextCalled = true
		return nil
	})

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !nextCalled {
		t.Error("next() should be called after processing audio")
	}

	if len(ctx.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(ctx.Messages))
	}
}

func TestVADMiddleware_StreamChunk_NoOp(t *testing.T) {
	vad := &mockVADAnalyzer{}
	transcriber := &mockTranscriber{text: "Test"}
	middleware := NewVADMiddleware(vad, transcriber, DefaultVADConfig())

	ctx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	chunk := &providers.StreamChunk{
		Delta: "Output text",
	}

	// StreamChunk should be a no-op for VAD
	err := middleware.StreamChunk(ctx, chunk)
	if err != nil {
		t.Errorf("StreamChunk should return nil, got %v", err)
	}
}

func TestVADMiddleware_EmptyTranscription(t *testing.T) {
	vad := &mockVADAnalyzer{
		scores: []float64{0.8, 0.2, 0.2}, // Speech then silence
	}
	transcriber := &mockTranscriber{
		text: "", // Empty transcription
	}
	config := VADConfig{
		Threshold:       0.3,
		SilenceDuration: 50 * time.Millisecond,
	}
	middleware := NewVADMiddleware(vad, transcriber, config)

	streamInput := make(chan providers.StreamChunk, 10)
	go func() {
		for i := 0; i < 3; i++ {
			streamInput <- providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     func() *string { s := string([]byte{1, 2, 3}); return &s }(),
				},
			}
			time.Sleep(30 * time.Millisecond)
		}
		close(streamInput)
	}()

	ctx := &pipeline.ExecutionContext{
		Context:     context.Background(),
		StreamMode:  true,
		StreamInput: streamInput,
	}

	err := middleware.Process(ctx, func() error { return nil })
	if err == nil {
		t.Fatal("Expected error for empty transcription")
	}
	if err.Error() != "transcription returned empty text" {
		t.Errorf("Expected 'transcription returned empty text', got '%v'", err)
	}
}

func TestDefaultVADConfig(t *testing.T) {
	config := DefaultVADConfig()

	if config.Threshold != 0.3 {
		t.Errorf("Expected Threshold 0.3, got %f", config.Threshold)
	}
	if config.MinSpeechDuration != 300*time.Millisecond {
		t.Errorf("Expected MinSpeechDuration 300ms, got %v", config.MinSpeechDuration)
	}
	if config.MaxTurnDuration != 30*time.Second {
		t.Errorf("Expected MaxTurnDuration 30s, got %v", config.MaxTurnDuration)
	}
	if config.SilenceDuration != 700*time.Millisecond {
		t.Errorf("Expected SilenceDuration 700ms, got %v", config.SilenceDuration)
	}
}
