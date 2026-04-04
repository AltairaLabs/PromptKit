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

// helperMockSTTService implements stt.Service with a configurable transcribeFunc.
type helperMockSTTService struct {
	transcribeFunc func(ctx context.Context, audio []byte, config stt.TranscriptionConfig) (string, error)
}

func (m *helperMockSTTService) Name() string { return "mock-stt" }
func (m *helperMockSTTService) SupportedFormats() []string {
	return []string{"pcm", "wav"}
}
func (m *helperMockSTTService) Transcribe(ctx context.Context, audioData []byte, config stt.TranscriptionConfig) (string, error) {
	if m.transcribeFunc != nil {
		return m.transcribeFunc(ctx, audioData, config)
	}
	return "Test transcription", nil
}

// =============================================================================
// Mock TTS service
// =============================================================================

// helperMockTTSService implements tts.Service with a configurable synthesizeFunc.
type helperMockTTSService struct {
	synthesizeFunc func(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error)
}

func (m *helperMockTTSService) Name() string { return "mock-tts" }
func (m *helperMockTTSService) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "test", Name: "Test Voice"}}
}
func (m *helperMockTTSService) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{tts.FormatPCM16}
}
func (m *helperMockTTSService) Synthesize(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
	if m.synthesizeFunc != nil {
		return m.synthesizeFunc(ctx, text, config)
	}
	return io.NopCloser(strings.NewReader(helperGenerateTestPCM(100))), nil
}

// =============================================================================
// Mock VAD analyzer
// =============================================================================

// helperMockVADAnalyzer implements audio.VADAnalyzer, playing back a predefined
// state sequence. An optional analyzeFunc overrides the default probability logic.
type helperMockVADAnalyzer struct {
	states        []audio.VADState
	currentIdx    int
	currentState  audio.VADState
	stateChangeCh chan audio.VADEvent
	analyzeFunc   func(ctx context.Context, audioData []byte) (float64, error)
}

func (m *helperMockVADAnalyzer) Name() string { return "mock-vad" }

func (m *helperMockVADAnalyzer) Analyze(ctx context.Context, audioData []byte) (float64, error) {
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

func (m *helperMockVADAnalyzer) State() audio.VADState {
	return m.currentState
}

func (m *helperMockVADAnalyzer) OnStateChange() <-chan audio.VADEvent {
	if m.stateChangeCh == nil {
		m.stateChangeCh = make(chan audio.VADEvent, 10)
	}
	return m.stateChangeCh
}

func (m *helperMockVADAnalyzer) Reset() {
	m.currentIdx = 0
	m.currentState = audio.VADStateQuiet
}

// =============================================================================
// Mock turn detector
// =============================================================================

// helperMockTurnDetector implements audio.TurnDetector with configurable behavior.
type helperMockTurnDetector struct {
	isUserSpeakingVal bool
	processAudioFunc  func(ctx context.Context, audio []byte) (bool, error)
	resetCalled       bool
}

func (m *helperMockTurnDetector) Name() string { return "mock-turn-detector" }

func (m *helperMockTurnDetector) ProcessAudio(ctx context.Context, audioData []byte) (bool, error) {
	if m.processAudioFunc != nil {
		return m.processAudioFunc(ctx, audioData)
	}
	return false, nil
}

func (m *helperMockTurnDetector) ProcessVADState(_ context.Context, _ audio.VADState) (bool, error) {
	return false, nil
}

func (m *helperMockTurnDetector) IsUserSpeaking() bool {
	return m.isUserSpeakingVal
}

func (m *helperMockTurnDetector) Reset() {
	m.resetCalled = true
}

// =============================================================================
// errorReader — io.Reader that always returns an error
// =============================================================================

// helperErrorReader is a reader that always returns an error on Read.
type helperErrorReader struct {
	err error
}

func (r *helperErrorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

// =============================================================================
// PCM generation helpers
// =============================================================================

// helperGenerateTestPCM generates test PCM data as a string of the given length.
func helperGenerateTestPCM(length int) string {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return string(data)
}

// helperGenerateTestPCMAudio generates test PCM audio as a byte slice of the given length.
func helperGenerateTestPCMAudio(length int) []byte {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// =============================================================================
// Stage execution helper
// =============================================================================

// helperRunStage sends inputs through a stage's Process, collects all output
// elements, and returns them. It waits up to timeout for each element and
// terminates once the output channel is closed or the timeout elapses.
func helperRunStage(t *testing.T, s stage.Stage, inputs []stage.StreamElement, timeout time.Duration) []stage.StreamElement {
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

// helperMakeAudioElement constructs a StreamElement carrying PCM16 audio.
func helperMakeAudioElement(samples []byte, sampleRate int) stage.StreamElement {
	return stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    samples,
			SampleRate: sampleRate,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
	}
}

// helperMakeTextElement constructs a StreamElement carrying a text string.
func helperMakeTextElement(text string) stage.StreamElement {
	s := text
	return stage.StreamElement{Text: &s}
}

// helperMakeEndOfStreamElement constructs an end-of-stream StreamElement.
func helperMakeEndOfStreamElement() stage.StreamElement {
	return stage.NewEndOfStreamElement()
}
