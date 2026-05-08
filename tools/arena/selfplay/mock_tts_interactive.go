package selfplay

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// Compile-time check: MockTTSService must satisfy base.TTSProvider.
var _ base.TTSProvider = (*MockTTSService)(nil)

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
	// mockTTSReadChunkBytes caps how much one Read returns. Sized at 20 ms
	// of mono s16le at the default sample rate so a caller passing a
	// large buffer still drains the audio in audio-chunk-shaped reads
	// rather than one giant read — matching how a real TTS HTTP body
	// arrives in chunked-transfer frames. Pacing is the AudioPacingStage's
	// job; this just keeps the read shape sane.
	mockTTSReadChunkBytes = mockTTSDefaultSampleRate * mockTTSBytesPerSample / 50
)

// MockTTSService is a mock TTS service for testing.
// It loads audio from PCM files to provide realistic speech for testing.
type MockTTSService struct {
	// SampleRate is the audio sample rate (default: 24000).
	SampleRate int

	// AudioFiles is a list of PCM audio files to load and rotate through.
	// If empty, falls back to generating silence.
	AudioFiles []string

	// Latency simulates network/processing delay before returning audio.
	Latency time.Duration

	// mu guards mutable state (cachedAudio + currentFileIndex). Synthesize
	// is part of a public interface that may be called concurrently
	// (multiple parallel scenario runs hitting the same mock instance);
	// without this lock the file-rotation index races and
	// `go test -race` fails.
	mu               sync.Mutex
	currentFileIndex int
	cachedAudio      [][]byte
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

// loadAudioFiles loads audio data from configured files. Called only
// from constructors before the mock is exposed to other goroutines, so
// no lock is needed here.
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
// Returns a chunked reader that caps each Read at ~20 ms of audio so a
// caller using a large buffer (e.g. io.ReadAll) still observes
// chunk-shaped reads. Pacing — the actual wall-clock cadence — is the
// AudioPacingStage's job, applied downstream by the duplex pipeline.
//
//nolint:gocritic // hugeParam: interface compliance requires value receiver for config
func (m *MockTTSService) Synthesize(
	ctx context.Context, text string, _ tts.SynthesisConfig,
) (io.ReadCloser, error) {
	// Simulate latency
	if m.Latency > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.Latency):
		}
	}

	// Use cached audio files if available. Pick + advance the rotation
	// index under the lock so concurrent Synthesize calls don't race.
	m.mu.Lock()
	if len(m.cachedAudio) > 0 {
		audio := m.cachedAudio[m.currentFileIndex]
		m.currentFileIndex = (m.currentFileIndex + 1) % len(m.cachedAudio)
		m.mu.Unlock()
		return newChunkedBytesReadCloser(audio, mockTTSReadChunkBytes), nil
	}
	m.mu.Unlock()

	// Fall back to generating silence (text-only — no shared state).
	return newChunkedBytesReadCloser(m.generatePCMAudio(text), mockTTSReadChunkBytes), nil
}

// chunkedBytesReader returns up to chunkSize bytes per Read so callers
// drain the audio in audio-chunk-shaped reads rather than one giant
// read. No pacing — that's the AudioPacingStage's job.
type chunkedBytesReader struct {
	data      []byte
	chunkSize int
	off       int
}

func newChunkedBytesReadCloser(data []byte, chunkSize int) io.ReadCloser {
	return io.NopCloser(&chunkedBytesReader{data: data, chunkSize: chunkSize})
}

func (c *chunkedBytesReader) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, io.EOF
	}
	n := len(p)
	if n > c.chunkSize {
		n = c.chunkSize
	}
	if c.off+n > len(c.data) {
		n = len(c.data) - c.off
	}
	copy(p[:n], c.data[c.off:c.off+n])
	c.off += n
	return n, nil
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

// Type returns ProviderTypeTTS for MockTTSService.
func (m *MockTTSService) Type() base.ProviderType { return base.ProviderTypeTTS }

// Pricing returns nil for the mock (free provider, no billing).
func (m *MockTTSService) Pricing() *base.PricingDescriptor { return nil }

// Validate performs synchronous config validation (no-op for mock).
func (m *MockTTSService) Validate() error { return nil }

// Init performs asynchronous setup (no-op for mock).
func (m *MockTTSService) Init(_ context.Context) error { return nil }

// HealthCheck reports liveness (no-op for mock).
func (m *MockTTSService) HealthCheck(_ context.Context) error { return nil }

// Close releases resources (no-op for mock).
func (m *MockTTSService) Close() error { return nil }

// SynthesizeTTS implements base.TTSProvider. It bridges the base.TTSRequest
// to the existing Synthesize method and wraps the response in a TTSStream.
// Cost is nil because the mock has no pricing.
func (m *MockTTSService) SynthesizeTTS(ctx context.Context, req base.TTSRequest) (base.TTSStream, error) {
	reader, err := m.Synthesize(ctx, req.Text, tts.SynthesisConfig{Voice: req.Voice})
	if err != nil {
		return nil, err
	}
	// Wrap the io.ReadCloser in a TTSStream. Mock has no pricing so cost is nil.
	return newMockTTSStream(reader), nil
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
