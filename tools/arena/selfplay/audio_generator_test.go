package selfplay

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

// mockProvider implements providers.Provider for testing.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) ID() string                          { return "mock" }
func (m *mockProvider) Name() string                        { return "mock" }
func (m *mockProvider) Type() base.ProviderType             { return base.ProviderTypeInference }
func (m *mockProvider) Pricing() *base.PricingDescriptor    { return nil }
func (m *mockProvider) Validate() error                     { return nil }
func (m *mockProvider) Init(_ context.Context) error        { return nil }
func (m *mockProvider) HealthCheck(_ context.Context) error { return nil }
func (m *mockProvider) Model() string                       { return "mock-model" }
func (m *mockProvider) SupportsStreaming() bool             { return false }
func (m *mockProvider) ShouldIncludeRawOutput() bool        { return false }
func (m *mockProvider) Close() error                        { return nil }
func (m *mockProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func (m *mockProvider) Predict(
	_ context.Context,
	_ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	if m.err != nil {
		return providers.PredictionResponse{}, m.err
	}
	return providers.PredictionResponse{
		Content: m.response,
	}, nil
}

func (m *mockProvider) PredictStream(
	_ context.Context,
	_ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	return nil, errors.New("streaming not supported")
}

// mockTTSServiceWithData is a minimal base.TTSProvider that returns specified audio data.
type mockTTSServiceWithData struct {
	audioData []byte
	err       error
}

func (m *mockTTSServiceWithData) Name() string                        { return "mock-tts" }
func (m *mockTTSServiceWithData) Type() base.ProviderType             { return base.ProviderTypeTTS }
func (m *mockTTSServiceWithData) Pricing() *base.PricingDescriptor    { return nil }
func (m *mockTTSServiceWithData) Validate() error                     { return nil }
func (m *mockTTSServiceWithData) Init(_ context.Context) error        { return nil }
func (m *mockTTSServiceWithData) HealthCheck(_ context.Context) error { return nil }
func (m *mockTTSServiceWithData) Close() error                        { return nil }

func (m *mockTTSServiceWithData) SynthesizeTTS(_ context.Context, _ base.TTSRequest) (base.TTSStream, error) {
	if m.err != nil {
		return nil, m.err
	}
	return newMockTTSStream(io.NopCloser(strings.NewReader(string(m.audioData)))), nil
}

// drainStream reads the AudioStreamResult.Reader to completion and returns
// the bytes. Closes the reader. Used by tests that want to assert on the
// full synthesized payload without re-introducing a buffered codepath in
// production code.
func drainStream(t *testing.T, r io.ReadCloser) []byte {
	t.Helper()
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("drain audio stream: %v", err)
	}
	return data
}

// newTestTTSProvider returns a minimal *config.Provider suitable for
// constructing an AudioContentGenerator in tests.
func newTestTTSProvider(voice string, sampleRate int) *config.Provider {
	return &config.Provider{
		ID:         "tts-test",
		Type:       TTSProviderMock,
		Role:       config.RoleTTS,
		Voice:      voice,
		SampleRate: sampleRate,
	}
}

func TestAudioContentGenerator_NextUserTurnAudioStream(t *testing.T) {
	mockProv := &mockProvider{response: "Hello, how can I help?"}

	defaultTemp := arenaconfig.DefaultPersonaTemperature
	persona := &arenaconfig.UserPersonaPack{
		ID: "test-persona",
		Defaults: arenaconfig.PersonaDefaults{
			Temperature: &defaultTemp,
		},
		SystemPrompt: "You are a helpful assistant",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{
		audioData: []byte("fake-audio-data"),
	}
	ttsProvider := newTestTTSProvider("test-voice", 0)
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsProvider)

	stream, err := audioGen.NextUserTurnAudioStream(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil,
	)
	if err != nil {
		t.Fatalf("NextUserTurnAudioStream() error = %v", err)
	}
	if stream.TextResult == nil {
		t.Error("NextUserTurnAudioStream() TextResult is nil")
	}
	got := drainStream(t, stream.Reader)
	if string(got) != "fake-audio-data" {
		t.Errorf("audio = %q, want %q", got, "fake-audio-data")
	}
}

func TestAudioContentGenerator_TTSError(t *testing.T) {
	mockProv := &mockProvider{response: "Hello"}
	persona := &arenaconfig.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "Test",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{
		err: errors.New("TTS service unavailable"),
	}
	ttsProvider := newTestTTSProvider("test-voice", 0)
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsProvider)

	_, err := audioGen.NextUserTurnAudioStream(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil,
	)
	if err == nil {
		t.Fatal("expected error for TTS failure, got nil")
	}
	if !strings.Contains(err.Error(), "TTS service unavailable") {
		t.Errorf("error = %v, want one wrapping TTS service failure", err)
	}
}

func TestAudioContentGenerator_TextGenerationError(t *testing.T) {
	mockProv := &mockProvider{err: errors.New("LLM unavailable")}
	persona := &arenaconfig.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "Test",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{audioData: []byte("audio")}
	ttsProvider := newTestTTSProvider("test", 0)
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsProvider)

	_, err := audioGen.NextUserTurnAudioStream(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil,
	)
	if err == nil {
		t.Fatal("expected error for text generation failure")
	}
	if !strings.Contains(err.Error(), "failed to generate text") {
		t.Errorf("error = %v, want one containing 'failed to generate text'", err)
	}
}

func TestAudioContentGenerator_EmptyTextResponse(t *testing.T) {
	mockProv := &mockProvider{response: ""}
	persona := &arenaconfig.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "Test",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{audioData: []byte("audio")}
	ttsProvider := newTestTTSProvider("test", 0)
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsProvider)

	_, err := audioGen.NextUserTurnAudioStream(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil,
	)
	if err == nil {
		t.Fatal("expected error for empty text response")
	}
	if !strings.Contains(err.Error(), "no text content generated") {
		t.Errorf("error = %v, want one containing 'no text content generated'", err)
	}
}

func TestAudioContentGenerator_SynthesizeTextStream(t *testing.T) {
	mockTTS := &mockTTSServiceWithData{audioData: []byte("scripted-audio")}
	ttsProvider := newTestTTSProvider("test", 0)
	audioGen := NewAudioContentGenerator(nil, mockTTS, ttsProvider)

	stream, err := audioGen.SynthesizeTextStream(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("SynthesizeTextStream() error = %v", err)
	}
	if stream.TextResult != nil {
		t.Error("SynthesizeTextStream() should not produce a TextResult (no LLM ran)")
	}
	got := drainStream(t, stream.Reader)
	if string(got) != "scripted-audio" {
		t.Errorf("audio = %q, want %q", got, "scripted-audio")
	}
}

func TestAudioContentGenerator_GetTTSService(t *testing.T) {
	mockTTS := &mockTTSServiceWithData{}
	ttsProvider := newTestTTSProvider("", 0)
	audioGen := NewAudioContentGenerator(nil, mockTTS, ttsProvider)

	svc := audioGen.GetTTSService()
	if svc != mockTTS {
		t.Error("GetTTSService() returned wrong service")
	}
}

func TestAudioContentGenerator_GetTextGenerator(t *testing.T) {
	textGen := &ContentGenerator{}
	ttsProvider := newTestTTSProvider("", 0)
	audioGen := NewAudioContentGenerator(textGen, nil, ttsProvider)

	gen := audioGen.GetTextGenerator()
	if gen != textGen {
		t.Error("GetTextGenerator() returned wrong generator")
	}
}

// TestAudioContentGenerator_SynthesizeTextStream_WithProvider verifies that the
// constructor wires voice and sample_rate from the *config.Provider.
func TestAudioContentGenerator_SynthesizeTextStream_WithProvider(t *testing.T) {
	mockTTS := &mockTTSServiceWithData{audioData: []byte("provider-path-audio")}
	p := &config.Provider{
		ID:         "tts-test",
		Type:       TTSProviderMock,
		Role:       config.RoleTTS,
		Voice:      "test-voice",
		SampleRate: 16000,
	}
	audioGen := NewAudioContentGenerator(nil, mockTTS, p)

	stream, err := audioGen.SynthesizeTextStream(context.Background(), "hello from provider path")
	if err != nil {
		t.Fatalf("SynthesizeTextStream() error = %v", err)
	}
	got := drainStream(t, stream.Reader)
	if string(got) != "provider-path-audio" {
		t.Errorf("audio = %q, want %q", got, "provider-path-audio")
	}
	if stream.SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", stream.SampleRate)
	}
}

// TestAudioContentGenerator_NextUserTurnAudioStream_WithProvider verifies the
// full text-generation + TTS synthesis path.
func TestAudioContentGenerator_NextUserTurnAudioStream_WithProvider(t *testing.T) {
	mockProv := &mockProvider{response: "response from provider path"}
	defaultTemp := arenaconfig.DefaultPersonaTemperature
	persona := &arenaconfig.UserPersonaPack{
		ID: "persona-provider-path",
		Defaults: arenaconfig.PersonaDefaults{
			Temperature: &defaultTemp,
		},
		SystemPrompt: "You are helpful",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{audioData: []byte("synthesized")}
	p := &config.Provider{
		ID:         "tts-provider",
		Type:       TTSProviderMock,
		Role:       config.RoleTTS,
		Voice:      "nova",
		SampleRate: 24000,
	}
	audioGen := NewAudioContentGenerator(textGen, mockTTS, p)

	stream, err := audioGen.NextUserTurnAudioStream(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil,
	)
	if err != nil {
		t.Fatalf("NextUserTurnAudioStream() error = %v", err)
	}
	if stream.TextResult == nil {
		t.Error("expected TextResult to be set")
	}
	got := drainStream(t, stream.Reader)
	if string(got) != "synthesized" {
		t.Errorf("audio = %q, want %q", got, "synthesized")
	}
}

// TestAudioContentGenerator_DefaultSampleRate verifies that a provider with
// SampleRate == 0 falls back to the default 24 kHz value.
func TestAudioContentGenerator_DefaultSampleRate(t *testing.T) {
	mockTTS := &mockTTSServiceWithData{audioData: []byte("audio")}
	p := &config.Provider{
		ID:   "tts-no-rate",
		Type: TTSProviderMock,
		Role: config.RoleTTS,
		// SampleRate deliberately omitted — should use defaultTTSSampleRate
	}
	audioGen := NewAudioContentGenerator(nil, mockTTS, p)

	stream, err := audioGen.SynthesizeTextStream(context.Background(), "test")
	if err != nil {
		t.Fatalf("SynthesizeTextStream() error = %v", err)
	}
	drainStream(t, stream.Reader)
	if stream.SampleRate != defaultTTSSampleRate {
		t.Errorf("SampleRate = %d, want %d", stream.SampleRate, defaultTTSSampleRate)
	}
}
