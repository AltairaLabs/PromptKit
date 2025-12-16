package stage_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock STT service for testing
type mockSTTService struct {
	transcribeFunc func(ctx context.Context, audio []byte, config stt.TranscriptionConfig) (string, error)
}

func (m *mockSTTService) Name() string { return "mock-stt" }
func (m *mockSTTService) SupportedFormats() []string {
	return []string{"pcm", "wav"}
}
func (m *mockSTTService) Transcribe(ctx context.Context, audio []byte, config stt.TranscriptionConfig) (string, error) {
	if m.transcribeFunc != nil {
		return m.transcribeFunc(ctx, audio, config)
	}
	return "Test transcription", nil
}

// Mock TTS service for testing
type mockTTSService struct {
	synthesizeFunc func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error)
}

func (m *mockTTSService) Name() string { return "mock-tts" }
func (m *mockTTSService) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "test", Name: "Test Voice"}}
}
func (m *mockTTSService) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{tts.FormatPCM16}
}
func (m *mockTTSService) Synthesize(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
	if m.synthesizeFunc != nil {
		return m.synthesizeFunc(ctx, text, config)
	}
	// Return mock audio data
	return io.NopCloser(strings.NewReader(generateTestPCM(100))), nil
}

// Test AudioTurnStage

func TestNewAudioTurnStage(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	s, err := stage.NewAudioTurnStage(config)
	if err != nil {
		t.Fatalf("NewAudioTurnStage failed: %v", err)
	}

	if s.Type() != stage.StageTypeAccumulate {
		t.Errorf("Type() = %v, want StageTypeAccumulate", s.Type())
	}

	if s.Name() != "audio_turn" {
		t.Errorf("Name() = %q, want %q", s.Name(), "audio_turn")
	}
}

func TestAudioTurnStage_PassThroughNonAudio(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	s, err := stage.NewAudioTurnStage(config)
	if err != nil {
		t.Fatalf("NewAudioTurnStage failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 1)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send non-audio element
	text := "Hello, world!"
	input <- stage.StreamElement{Text: &text}
	close(input)

	// Should pass through
	select {
	case elem := <-output:
		if elem.Text == nil || *elem.Text != "Hello, world!" {
			t.Errorf("Expected text element, got: %+v", elem)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestAudioTurnStage_AccumulatesAudio(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.SilenceDuration = 100 * time.Millisecond
	config.MinSpeechDuration = 50 * time.Millisecond

	// Create a mock VAD that reports speaking then silence
	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateStopping,
			audio.VADStateQuiet,
			audio.VADStateQuiet, // Extended silence
			audio.VADStateQuiet,
		},
		currentIdx: 0,
	}
	config.VAD = mockVAD

	s, err := stage.NewAudioTurnStage(config)
	if err != nil {
		t.Fatalf("NewAudioTurnStage failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send audio chunks
	for i := 0; i < 6; i++ {
		audioData := generateTestPCMAudio(160) // 10ms at 16kHz
		input <- stage.StreamElement{
			Audio: &stage.AudioData{
				Samples:    audioData,
				SampleRate: 16000,
				Channels:   1,
				Format:     stage.AudioFormatPCM16,
			},
		}
		time.Sleep(20 * time.Millisecond) // Simulate real-time
	}
	close(input)

	// Should receive accumulated audio
	select {
	case elem := <-output:
		if elem.Audio == nil {
			t.Fatal("Expected audio output, got nil")
		}
		if len(elem.Audio.Samples) == 0 {
			t.Error("Expected non-empty audio samples")
		}
		// Check turn_complete metadata
		if elem.Metadata == nil {
			t.Error("Expected metadata with turn_complete")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

// Test STTStage

func TestNewSTTStage(t *testing.T) {
	mockStt := &mockSTTService{}
	config := stage.DefaultSTTStageConfig()

	s := stage.NewSTTStage(mockStt, config)

	if s.Type() != stage.StageTypeTransform {
		t.Errorf("Type() = %v, want StageTypeTransform", s.Type())
	}

	if s.Name() != "stt" {
		t.Errorf("Name() = %q, want %q", s.Name(), "stt")
	}
}

func TestSTTStage_TranscribesAudio(t *testing.T) {
	mockStt := &mockSTTService{
		transcribeFunc: func(ctx context.Context, audio []byte, config stt.TranscriptionConfig) (string, error) {
			return "Hello from transcription", nil
		},
	}
	config := stage.DefaultSTTStageConfig()

	s := stage.NewSTTStage(mockStt, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 1)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send audio element
	audioData := generateTestPCMAudio(32000) // 1 second at 16kHz 16-bit
	input <- stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    audioData,
			SampleRate: 16000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
	}
	close(input)

	// Should receive text
	select {
	case elem := <-output:
		if elem.Text == nil {
			t.Fatal("Expected text output, got nil")
		}
		if *elem.Text != "Hello from transcription" {
			t.Errorf("Text = %q, want %q", *elem.Text, "Hello from transcription")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestSTTStage_SkipsSmallAudio(t *testing.T) {
	mockStt := &mockSTTService{
		transcribeFunc: func(ctx context.Context, audio []byte, config stt.TranscriptionConfig) (string, error) {
			t.Error("Transcribe should not be called for small audio")
			return "", nil
		},
	}
	config := stage.DefaultSTTStageConfig()
	config.MinAudioBytes = 1000

	s := stage.NewSTTStage(mockStt, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 1)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send small audio element
	input <- stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    make([]byte, 100), // Too small
			SampleRate: 16000,
			Channels:   1,
		},
	}
	close(input)

	// Should not receive anything (small audio is skipped)
	select {
	case elem := <-output:
		if elem.Text != nil {
			t.Errorf("Unexpected output for small audio: %+v", elem)
		}
	case <-time.After(100 * time.Millisecond):
		// Expected - no output
	}
}

func TestSTTStage_PassesThroughNonAudio(t *testing.T) {
	mockStt := &mockSTTService{}
	config := stage.DefaultSTTStageConfig()

	s := stage.NewSTTStage(mockStt, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 1)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send non-audio element
	text := "Pass through text"
	input <- stage.StreamElement{Text: &text}
	close(input)

	// Should pass through unchanged
	select {
	case elem := <-output:
		if elem.Text == nil || *elem.Text != "Pass through text" {
			t.Errorf("Expected passthrough, got: %+v", elem)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

// Test TTSStageWithInterruption

func TestNewTTSStageWithInterruption(t *testing.T) {
	mockTts := &mockTTSService{}
	config := stage.DefaultTTSStageWithInterruptionConfig()

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	if s.Type() != stage.StageTypeTransform {
		t.Errorf("Type() = %v, want StageTypeTransform", s.Type())
	}

	if s.Name() != "tts_interruptible" {
		t.Errorf("Name() = %q, want %q", s.Name(), "tts_interruptible")
	}
}

func TestTTSStageWithInterruption_SynthesizesText(t *testing.T) {
	audioOutput := "test audio data"
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(audioOutput)), nil
		},
	}
	config := stage.DefaultTTSStageWithInterruptionConfig()

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 1)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send text element
	text := "Hello, I am speaking"
	input <- stage.StreamElement{Text: &text}
	close(input)

	// Should receive audio
	select {
	case elem := <-output:
		if elem.Audio == nil {
			t.Fatal("Expected audio output, got nil")
		}
		if len(elem.Audio.Samples) == 0 {
			t.Error("Expected non-empty audio samples")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestTTSStageWithInterruption_PassesThroughNonText(t *testing.T) {
	mockTts := &mockTTSService{}
	config := stage.DefaultTTSStageWithInterruptionConfig()

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 1)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send audio element (should pass through)
	audioData := make([]byte, 100)
	input <- stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    audioData,
			SampleRate: 16000,
		},
	}
	close(input)

	// Should pass through unchanged
	select {
	case elem := <-output:
		if elem.Audio == nil {
			t.Error("Expected audio passthrough")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestTTSStageWithInterruption_WithInterruption(t *testing.T) {
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}

	// Create interruption handler
	handler := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	handler.SetBotSpeaking(true)

	config := stage.DefaultTTSStageWithInterruptionConfig()
	config.InterruptionHandler = handler

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	// Simulate interruption by marking it before processing
	// The interruption handler will report WasInterrupted() = true
	// after ProcessVADState is called with VADStateSpeaking
	_, _ = handler.ProcessVADState(ctx, audio.VADStateSpeaking)

	go func() {
		err := s.Process(ctx, input, output)
		if err != nil && err != context.Canceled {
			t.Errorf("Process error: %v", err)
		}
	}()

	// Send text element
	text := "This should be interrupted"
	input <- stage.StreamElement{Text: &text}
	close(input)

	// Wait a bit for processing
	time.Sleep(200 * time.Millisecond)

	// With interruption, the text should be skipped
	// The handler should have detected the interruption and skip synthesis
}

// Test DefaultConfigs

func TestDefaultAudioTurnConfig(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()

	if config.SilenceDuration == 0 {
		t.Error("SilenceDuration should have default value")
	}
	if config.MinSpeechDuration == 0 {
		t.Error("MinSpeechDuration should have default value")
	}
	if config.MaxTurnDuration == 0 {
		t.Error("MaxTurnDuration should have default value")
	}
	if config.SampleRate == 0 {
		t.Error("SampleRate should have default value")
	}
}

func TestDefaultSTTStageConfig(t *testing.T) {
	config := stage.DefaultSTTStageConfig()

	if config.Language != "en" {
		t.Errorf("Language = %q, want %q", config.Language, "en")
	}
	if !config.SkipEmpty {
		t.Error("SkipEmpty should default to true")
	}
	if config.MinAudioBytes == 0 {
		t.Error("MinAudioBytes should have default value")
	}
}

func TestDefaultTTSStageWithInterruptionConfig(t *testing.T) {
	config := stage.DefaultTTSStageWithInterruptionConfig()

	if config.Voice == "" {
		t.Error("Voice should have default value")
	}
	if config.Speed == 0 {
		t.Error("Speed should have default value")
	}
	if !config.SkipEmpty {
		t.Error("SkipEmpty should default to true")
	}
}

// Helper types and functions

// mockVADAnalyzer is a mock VAD for testing
type mockVADAnalyzer struct {
	states        []audio.VADState
	currentIdx    int
	currentState  audio.VADState
	stateChangeCh chan audio.VADEvent
}

func (m *mockVADAnalyzer) Name() string { return "mock-vad" }

func (m *mockVADAnalyzer) Analyze(ctx context.Context, audioData []byte) (float64, error) {
	// Update state based on predefined sequence
	if m.currentIdx < len(m.states) {
		m.currentState = m.states[m.currentIdx]
		m.currentIdx++
	} else {
		m.currentState = audio.VADStateQuiet
	}

	// Return probability based on state
	switch m.currentState {
	case audio.VADStateSpeaking:
		return 0.9, nil
	case audio.VADStateStarting:
		return 0.6, nil
	case audio.VADStateStopping:
		return 0.3, nil
	default:
		return 0.1, nil
	}
}

func (m *mockVADAnalyzer) State() audio.VADState {
	return m.currentState
}

func (m *mockVADAnalyzer) OnStateChange() <-chan audio.VADEvent {
	if m.stateChangeCh == nil {
		m.stateChangeCh = make(chan audio.VADEvent, 10)
	}
	return m.stateChangeCh
}

func (m *mockVADAnalyzer) Reset() {
	m.currentIdx = 0
	m.currentState = audio.VADStateQuiet
}

// generateTestPCM generates test PCM data as string
func generateTestPCM(length int) string {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return string(data)
}

// generateTestPCMAudio generates test PCM audio bytes
func generateTestPCMAudio(length int) []byte {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// =============================================================================
// Additional TTSStageWithInterruption Tests (for coverage)
// =============================================================================

func TestTTSStageWithInterruption_SkipsEmptyText(t *testing.T) {
	synthesizeCalled := false
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesizeCalled = true
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	config := stage.DefaultTTSStageWithInterruptionConfig()
	config.SkipEmpty = true
	config.MinTextLength = 5

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send short text (should be skipped)
	text := "Hi"
	input <- stage.StreamElement{Text: &text}
	close(input)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	if synthesizeCalled {
		t.Error("Synthesize should not be called for short text")
	}
}

func TestTTSStageWithInterruption_SynthesisError(t *testing.T) {
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			return nil, io.EOF // Simulate error
		},
	}
	config := stage.DefaultTTSStageWithInterruptionConfig()

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	text := "Test synthesis error"
	input <- stage.StreamElement{Text: &text}
	close(input)

	// Should receive error element
	select {
	case elem := <-output:
		if elem.Error == nil {
			t.Error("Expected error element for synthesis failure")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestTTSStageWithInterruption_ContextCancellation(t *testing.T) {
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			// Simulate slow synthesis
			time.Sleep(500 * time.Millisecond)
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	config := stage.DefaultTTSStageWithInterruptionConfig()

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Process(ctx, input, output)
	}()

	text := "Test context cancellation"
	input <- stage.StreamElement{Text: &text}

	// Cancel immediately
	cancel()
	close(input)

	// Should complete (with or without error)
	select {
	case <-errChan:
		// Expected - context cancelled
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout - process should have completed")
	}
}

func TestTTSStageWithInterruption_MultipleTexts(t *testing.T) {
	synthesizeCount := 0
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesizeCount++
			return io.NopCloser(strings.NewReader("audio data")), nil
		},
	}
	config := stage.DefaultTTSStageWithInterruptionConfig()

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 3)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send multiple text elements
	texts := []string{"First sentence", "Second sentence", "Third sentence"}
	for _, txt := range texts {
		t := txt
		input <- stage.StreamElement{Text: &t}
	}
	close(input)

	// Collect outputs
	var received int
	timeout := time.After(3 * time.Second)
	for {
		select {
		case elem, ok := <-output:
			if !ok {
				goto done
			}
			if elem.Audio != nil {
				received++
			}
		case <-timeout:
			goto done
		}
	}
done:

	if received != 3 {
		t.Errorf("Expected 3 audio outputs, got %d", received)
	}
	if synthesizeCount != 3 {
		t.Errorf("Expected 3 synthesize calls, got %d", synthesizeCount)
	}
}

func TestTTSStageWithInterruption_MessageElement(t *testing.T) {
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	config := stage.DefaultTTSStageWithInterruptionConfig()

	s := stage.NewTTSStageWithInterruption(mockTts, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send element with no text (tests skip path)
	input <- stage.StreamElement{Text: nil, Metadata: map[string]interface{}{}}
	close(input)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)
}

// TestTTSStageWithInterruption_ExtractText_FromMessage tests extractText with message content.
func TestTTSStageWithInterruption_ExtractText_FromMessage(t *testing.T) {
	synthesized := false
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesized = true
			assert.Equal(t, "Hello from message", text)
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}

	s := stage.NewTTSStageWithInterruption(mockTts, stage.DefaultTTSStageWithInterruptionConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send message element with content
	msg := &types.Message{
		Role:    "assistant",
		Content: "Hello from message",
	}
	input <- stage.StreamElement{Message: msg, Metadata: map[string]interface{}{}}
	close(input)

	// Collect outputs
	var results []stage.StreamElement
	timeout := time.After(300 * time.Millisecond)
loop:
	for {
		select {
		case elem, ok := <-output:
			if !ok {
				break loop
			}
			results = append(results, elem)
		case <-timeout:
			break loop
		}
	}

	// Should have synthesized the message content
	require.GreaterOrEqual(t, len(results), 1)
	assert.True(t, synthesized, "should have called synthesize")
}

// TestTTSStageWithInterruption_ExtractText_FromMessageParts tests extractText with message parts.
func TestTTSStageWithInterruption_ExtractText_FromMessageParts(t *testing.T) {
	synthesized := false
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesized = true
			assert.Equal(t, "Hello from part", text)
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}

	s := stage.NewTTSStageWithInterruption(mockTts, stage.DefaultTTSStageWithInterruptionConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send message element with parts
	partText := "Hello from part"
	msg := &types.Message{
		Role:    "assistant",
		Content: "", // Empty content, should fall through to parts
		Parts: []types.ContentPart{
			{Type: "text", Text: &partText},
		},
	}
	input <- stage.StreamElement{Message: msg, Metadata: map[string]interface{}{}}
	close(input)

	// Collect outputs
	var results []stage.StreamElement
	timeout := time.After(300 * time.Millisecond)
loop:
	for {
		select {
		case elem, ok := <-output:
			if !ok {
				break loop
			}
			results = append(results, elem)
		case <-timeout:
			break loop
		}
	}

	// Should have synthesized the part text
	require.GreaterOrEqual(t, len(results), 1)
	assert.True(t, synthesized, "should have called synthesize")
}

// TestTTSStageWithInterruption_ExtractText_EmptyMessageContent tests extractText with empty message.
func TestTTSStageWithInterruption_ExtractText_EmptyMessageContent(t *testing.T) {
	synthesized := false
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesized = true
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}

	s := stage.NewTTSStageWithInterruption(mockTts, stage.DefaultTTSStageWithInterruptionConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send message element with empty content and no parts
	msg := &types.Message{
		Role:    "assistant",
		Content: "",
	}
	input <- stage.StreamElement{Message: msg, Metadata: map[string]interface{}{}}
	close(input)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Should not have called TTS since text is empty
	assert.False(t, synthesized, "should not have called synthesize for empty text")
}

// TestTTSStageWithInterruption_PerformSynthesis_ReadError tests performSynthesis reader error path.
func TestTTSStageWithInterruption_PerformSynthesis_ReadError(t *testing.T) {
	synthesized := false
	mockTts := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesized = true
			// Return a reader that will fail on read
			return io.NopCloser(&errorReader{err: io.ErrUnexpectedEOF}), nil
		},
	}

	s := stage.NewTTSStageWithInterruption(mockTts, stage.DefaultTTSStageWithInterruptionConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	text := "Test text for read error"
	input <- stage.StreamElement{Text: &text, Metadata: map[string]interface{}{}}
	close(input)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Should have tried to synthesize
	assert.True(t, synthesized, "should have tried to synthesize")
}

// errorReader is a reader that always returns an error
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, r.err
}
