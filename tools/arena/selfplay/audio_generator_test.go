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

func TestAudioContentGenerator_NextUserTurnAudioStream(t *testing.T) {
	mockProv := &mockProvider{response: "Hello, how can I help?"}

	defaultTemp := config.DefaultPersonaTemperature
	persona := &config.UserPersonaPack{
		ID: "test-persona",
		Defaults: config.PersonaDefaults{
			Temperature: &defaultTemp,
		},
		SystemPrompt: "You are a helpful assistant",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{
		audioData: []byte("fake-audio-data"),
	}
	ttsConfig := &config.TTSConfig{Provider: "mock", Voice: "test-voice"}
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsConfig)

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
	persona := &config.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "Test",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{
		err: errors.New("TTS service unavailable"),
	}
	ttsConfig := &config.TTSConfig{Provider: "mock", Voice: "test-voice"}
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsConfig)

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
	persona := &config.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "Test",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{audioData: []byte("audio")}
	ttsConfig := &config.TTSConfig{Provider: "mock", Voice: "test"}
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsConfig)

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
	persona := &config.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "Test",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{audioData: []byte("audio")}
	ttsConfig := &config.TTSConfig{Provider: "mock", Voice: "test"}
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsConfig)

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
	ttsConfig := &config.TTSConfig{Provider: "mock", Voice: "test"}
	audioGen := NewAudioContentGenerator(nil, mockTTS, ttsConfig)

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
	audioGen := NewAudioContentGenerator(nil, mockTTS, &config.TTSConfig{})

	svc := audioGen.GetTTSService()
	if svc != mockTTS {
		t.Error("GetTTSService() returned wrong service")
	}
}

func TestAudioContentGenerator_GetTextGenerator(t *testing.T) {
	textGen := &ContentGenerator{}
	audioGen := NewAudioContentGenerator(textGen, nil, &config.TTSConfig{})

	gen := audioGen.GetTextGenerator()
	if gen != textGen {
		t.Error("GetTextGenerator() returned wrong generator")
	}
}

// TestNewAudioContentGeneratorForProvider_SynthesizeTextStream verifies that the
// provider-path constructor wires voice and sample_rate from the *config.Provider.
func TestNewAudioContentGeneratorForProvider_SynthesizeTextStream(t *testing.T) {
	mockTTS := &mockTTSServiceWithData{audioData: []byte("provider-path-audio")}
	p := &config.Provider{
		ID:         "tts-test",
		Type:       TTSProviderMock,
		Capability: config.CapabilityTTS,
		Voice:      "test-voice",
		SampleRate: 16000,
	}
	audioGen := NewAudioContentGeneratorForProvider(nil, mockTTS, p)

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

// TestNewAudioContentGeneratorForProvider_NextUserTurnAudioStream verifies the
// full text-generation + TTS synthesis path through the provider-path constructor.
func TestNewAudioContentGeneratorForProvider_NextUserTurnAudioStream(t *testing.T) {
	mockProv := &mockProvider{response: "response from provider path"}
	defaultTemp := config.DefaultPersonaTemperature
	persona := &config.UserPersonaPack{
		ID: "persona-provider-path",
		Defaults: config.PersonaDefaults{
			Temperature: &defaultTemp,
		},
		SystemPrompt: "You are helpful",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{audioData: []byte("synthesized")}
	p := &config.Provider{
		ID:         "tts-provider",
		Type:       TTSProviderMock,
		Capability: config.CapabilityTTS,
		Voice:      "nova",
		SampleRate: 24000,
	}
	audioGen := NewAudioContentGeneratorForProvider(textGen, mockTTS, p)

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

// TestNewAudioContentGeneratorForProvider_DefaultSampleRate verifies that a
// provider with SampleRate == 0 falls back to the default 24 kHz value.
func TestNewAudioContentGeneratorForProvider_DefaultSampleRate(t *testing.T) {
	mockTTS := &mockTTSServiceWithData{audioData: []byte("audio")}
	p := &config.Provider{
		ID:         "tts-no-rate",
		Type:       TTSProviderMock,
		Capability: config.CapabilityTTS,
		// SampleRate deliberately omitted — should use defaultTTSSampleRate
	}
	audioGen := NewAudioContentGeneratorForProvider(nil, mockTTS, p)

	stream, err := audioGen.SynthesizeTextStream(context.Background(), "test")
	if err != nil {
		t.Fatalf("SynthesizeTextStream() error = %v", err)
	}
	drainStream(t, stream.Reader)
	if stream.SampleRate != defaultTTSSampleRate {
		t.Errorf("SampleRate = %d, want %d", stream.SampleRate, defaultTTSSampleRate)
	}
}
