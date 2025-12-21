package selfplay

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

const (
	// TTSProviderMock is the provider name for the mock TTS service.
	TTSProviderMock = "mock"

	// mockTTSDefaultSampleRate is the default sample rate for mock TTS (24kHz).
	mockTTSDefaultSampleRate = 24000
	// mockTTSSamplesPerCharDivisor calculates samples per character (sampleRate / 10 = 0.1 sec per char).
	mockTTSSamplesPerCharDivisor = 10
	// mockTTSMinSamples is the minimum number of samples (0.2 seconds at 24kHz).
	mockTTSMinSamples = 4800
	// mockTTSBytesPerSample is bytes per sample for 16-bit PCM audio.
	mockTTSBytesPerSample = 2
)

// MockTTSService is a mock TTS service for testing.
// It loads audio from PCM files to provide realistic speech for testing.
type MockTTSService struct {
	// SampleRate is the audio sample rate (default: 24000).
	SampleRate int

	// AudioFiles is a list of PCM audio files to load and rotate through.
	// If empty, falls back to generating silence.
	AudioFiles []string

	// currentFileIndex tracks which audio file to use next.
	currentFileIndex int

	// Latency simulates network/processing delay before returning audio.
	Latency time.Duration

	// cachedAudio stores loaded audio data from files.
	cachedAudio [][]byte
}

// NewMockTTS creates a new mock TTS service with default settings.
func NewMockTTS() *MockTTSService {
	return &MockTTSService{
		SampleRate: mockTTSDefaultSampleRate,
	}
}

// NewMockTTSWithFiles creates a mock TTS service that loads audio from files.
func NewMockTTSWithFiles(audioFiles []string) *MockTTSService {
	m := &MockTTSService{
		SampleRate: mockTTSDefaultSampleRate,
		AudioFiles: audioFiles,
	}
	m.loadAudioFiles()
	return m
}

// NewMockTTSWithLatency creates a mock TTS service with simulated latency.
func NewMockTTSWithLatency(latency time.Duration) *MockTTSService {
	m := NewMockTTS()
	m.Latency = latency
	return m
}

// loadAudioFiles loads audio data from configured files.
func (m *MockTTSService) loadAudioFiles() {
	m.cachedAudio = make([][]byte, 0, len(m.AudioFiles))
	for _, file := range m.AudioFiles {
		// #nosec G304 -- Files are from trusted configuration, not user input
		data, err := os.ReadFile(file)
		if err != nil {
			// Skip files that can't be loaded
			continue
		}
		m.cachedAudio = append(m.cachedAudio, data)
	}
}

// Name returns the provider name.
func (m *MockTTSService) Name() string {
	return TTSProviderMock
}

// Synthesize converts text to mock PCM audio.
// If audio files are configured, returns audio from those files (rotating through them).
// Otherwise falls back to generating silence.
//
//nolint:gocritic // hugeParam: interface compliance requires value receiver for config
func (m *MockTTSService) Synthesize(
	ctx context.Context, text string, config tts.SynthesisConfig,
) (io.ReadCloser, error) {
	// Simulate latency
	if m.Latency > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.Latency):
		}
	}

	// Use cached audio files if available
	if len(m.cachedAudio) > 0 {
		audio := m.cachedAudio[m.currentFileIndex]
		m.currentFileIndex = (m.currentFileIndex + 1) % len(m.cachedAudio)
		return io.NopCloser(bytes.NewReader(audio)), nil
	}

	// Fall back to generating silence
	audio := m.generatePCMAudio(text)

	return io.NopCloser(bytes.NewReader(audio)), nil
}

// SupportedVoices returns available mock voices.
func (m *MockTTSService) SupportedVoices() []tts.Voice {
	return []tts.Voice{
		{ID: "mock-alloy", Name: "Mock Alloy", Language: "en", Gender: "neutral", Description: "Mock voice for testing"},
		{ID: "mock-echo", Name: "Mock Echo", Language: "en", Gender: "male", Description: "Mock male voice"},
		{ID: "mock-nova", Name: "Mock Nova", Language: "en", Gender: "female", Description: "Mock female voice"},
	}
}

// SupportedFormats returns supported audio output formats.
func (m *MockTTSService) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{
		tts.FormatPCM16,
		tts.FormatWAV,
	}
}

// generatePCMAudio generates 16-bit PCM audio data.
// Creates silence (all zeros) proportional to text length.
// This is a fallback when no audio files are configured.
// Note: Silence won't trigger VAD in speech models, so this should only be
// used when audio files are not available or for basic testing.
func (m *MockTTSService) generatePCMAudio(text string) []byte {
	sampleRate := m.SampleRate
	if sampleRate == 0 {
		sampleRate = mockTTSDefaultSampleRate
	}

	// ~0.1 seconds per character, minimum 0.2 seconds
	samplesPerChar := sampleRate / mockTTSSamplesPerCharDivisor
	numSamples := len(text) * samplesPerChar
	if numSamples < mockTTSMinSamples {
		numSamples = mockTTSMinSamples
	}

	// Generate 16-bit PCM samples (2 bytes per sample)
	// All zeros = silence
	audio := make([]byte, numSamples*mockTTSBytesPerSample)

	return audio
}
