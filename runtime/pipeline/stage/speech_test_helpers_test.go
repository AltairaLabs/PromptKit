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
)

// =============================================================================
// Mock STT service
// =============================================================================

// mockSTTService implements stt.Service with a configurable transcribeFunc.
type mockSTTService struct {
	transcribeFunc func(ctx context.Context, audio []byte, config stt.TranscriptionConfig) (string, error)
}

func (m *mockSTTService) Name() string { return "mock-stt" }
func (m *mockSTTService) SupportedFormats() []string {
	return []string{"pcm", "wav"}
}
func (m *mockSTTService) Transcribe(ctx context.Context, audioData []byte, config stt.TranscriptionConfig) (string, error) {
	if m.transcribeFunc != nil {
		return m.transcribeFunc(ctx, audioData, config)
	}
	return "Test transcription", nil
}

// =============================================================================
// Mock TTS service
// =============================================================================

// mockTTSService implements tts.Service with a configurable synthesizeFunc.
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
	return io.NopCloser(strings.NewReader(generateTestPCM(100))), nil
}

// =============================================================================
// Mock VAD analyzer
// =============================================================================

// mockVADAnalyzer implements audio.VADAnalyzer, playing back a predefined
// state sequence. An optional analyzeFunc overrides the default probability logic.
type mockVADAnalyzer struct {
	states        []audio.VADState
	currentIdx    int
	currentState  audio.VADState
	stateChangeCh chan audio.VADEvent
	analyzeFunc   func(ctx context.Context, audioData []byte) (float64, error)
}

func (m *mockVADAnalyzer) Name() string { return "mock-vad" }

func (m *mockVADAnalyzer) Analyze(ctx context.Context, audioData []byte) (float64, error) {
	if m.analyzeFunc != nil {
		return m.analyzeFunc(ctx, audioData)
	}

	// Advance through the predefined state sequence.
	if m.currentIdx < len(m.states) {
		m.currentState = m.states[m.currentIdx]
		m.currentIdx++
	} else {
		m.currentState = audio.VADStateQuiet
	}

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

// =============================================================================
// Mock turn detector
// =============================================================================

// mockTurnDetector implements audio.TurnDetector with configurable behavior.
type mockTurnDetector struct {
	isUserSpeakingVal bool
	processAudioFunc  func(ctx context.Context, audio []byte) (bool, error)
	resetCalled       bool
}

func (m *mockTurnDetector) Name() string { return "mock-turn-detector" }

func (m *mockTurnDetector) ProcessAudio(ctx context.Context, audioData []byte) (bool, error) {
	if m.processAudioFunc != nil {
		return m.processAudioFunc(ctx, audioData)
	}
	return false, nil
}

func (m *mockTurnDetector) ProcessVADState(_ context.Context, _ audio.VADState) (bool, error) {
	return false, nil
}

func (m *mockTurnDetector) IsUserSpeaking() bool {
	return m.isUserSpeakingVal
}

func (m *mockTurnDetector) Reset() {
	m.resetCalled = true
}

// =============================================================================
// errorReader — io.Reader that always returns an error
// =============================================================================

// errorReader is a reader that always returns an error on Read.
type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

// =============================================================================
// PCM generation helpers
// =============================================================================

// generateTestPCM generates test PCM data as a string of the given length.
func generateTestPCM(length int) string {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return string(data)
}

// generateTestPCMAudio generates test PCM audio as a byte slice of the given length.
func generateTestPCMAudio(length int) []byte {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// =============================================================================
// Stage execution helper
// =============================================================================

// runStage sends inputs through a stage's Process, collects all output
// elements, and returns them. It waits up to timeout for each element and
// terminates once the output channel is closed or the timeout elapses.
func runStage(t *testing.T, s stage.Stage, inputs []stage.StreamElement, timeout time.Duration) []stage.StreamElement {
	t.Helper()

	input := make(chan stage.StreamElement, len(inputs))
	output := make(chan stage.StreamElement, len(inputs)+10)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Process(ctx, input, output)
	}()

	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	var results []stage.StreamElement
	deadline := time.After(timeout)
	for {
		select {
		case elem, ok := <-output:
			if !ok {
				return results
			}
			results = append(results, elem)
		case <-deadline:
			return results
		}
	}
}

// =============================================================================
// StreamElement construction helpers
// =============================================================================

// makeAudioElement constructs a StreamElement carrying PCM16 audio.
func makeAudioElement(samples []byte, sampleRate int) stage.StreamElement {
	return stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    samples,
			SampleRate: sampleRate,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
	}
}

// makeTextElement constructs a StreamElement carrying a text string.
func makeTextElement(text string) stage.StreamElement {
	s := text
	return stage.StreamElement{Text: &s}
}

// makeEndOfStreamElement constructs an end-of-stream StreamElement.
func makeEndOfStreamElement() stage.StreamElement {
	return stage.NewEndOfStreamElement()
}
