package selfplay

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// mockProvider implements providers.Provider for testing.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) ID() string                   { return "mock" }
func (m *mockProvider) Model() string                { return "mock-model" }
func (m *mockProvider) SupportsStreaming() bool      { return false }
func (m *mockProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockProvider) Close() error                 { return nil }
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

// mockTTSServiceWithData is a mock TTS service that returns specified audio data.
type mockTTSServiceWithData struct {
	audioData []byte
	err       error
}

func (m *mockTTSServiceWithData) Name() string { return "mock-tts" }

func (m *mockTTSServiceWithData) Synthesize(
	_ context.Context,
	_ string,
	_ tts.SynthesisConfig,
) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	return io.NopCloser(strings.NewReader(string(m.audioData))), nil
}

func (m *mockTTSServiceWithData) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "test-voice", Name: "Test"}}
}

func (m *mockTTSServiceWithData) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{tts.FormatPCM16}
}

func TestAudioContentGenerator_NextUserTurnAudio(t *testing.T) {
	// Create mock provider that returns text
	mockProv := &mockProvider{response: "Hello, how can I help?"}

	// Create persona config
	persona := &config.UserPersonaPack{
		ID: "test-persona",
		Defaults: config.PersonaDefaults{
			Temperature: 0.7,
		},
		SystemPrompt: "You are a helpful assistant",
	}

	// Create text generator
	textGen := NewContentGenerator(mockProv, persona)

	// Create mock TTS service
	mockTTS := &mockTTSServiceWithData{
		audioData: []byte("fake-audio-data"),
	}

	// Create TTS config
	ttsConfig := &config.TTSConfig{
		Provider: "mock",
		Voice:    "test-voice",
	}

	// Create audio generator
	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsConfig)

	// Generate audio
	result, err := audioGen.NextUserTurnAudio(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil, // opts
	)

	if err != nil {
		t.Fatalf("NextUserTurnAudio() error = %v", err)
	}

	if result.TextResult == nil {
		t.Error("NextUserTurnAudio() TextResult is nil")
	}

	if len(result.Audio) == 0 {
		t.Error("NextUserTurnAudio() Audio is empty")
	}

	if string(result.Audio) != "fake-audio-data" {
		t.Errorf("NextUserTurnAudio() Audio = %s, want 'fake-audio-data'", result.Audio)
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

	ttsConfig := &config.TTSConfig{
		Provider: "mock",
		Voice:    "test-voice",
	}

	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsConfig)

	_, err := audioGen.NextUserTurnAudio(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil, // opts
	)

	if err == nil {
		t.Error("NextUserTurnAudio() expected error for TTS failure")
	}
	if !strings.Contains(err.Error(), "failed to synthesize audio") {
		t.Errorf("NextUserTurnAudio() error = %v, want error containing 'failed to synthesize audio'", err)
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

	_, err := audioGen.NextUserTurnAudio(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil, // opts
	)

	if err == nil {
		t.Error("NextUserTurnAudio() expected error for text generation failure")
	}
	if !strings.Contains(err.Error(), "failed to generate text") {
		t.Errorf("NextUserTurnAudio() error = %v, want error containing 'failed to generate text'", err)
	}
}

func TestAudioContentGenerator_EmptyTextResponse(t *testing.T) {
	mockProv := &mockProvider{response: ""} // Empty response
	persona := &config.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "Test",
	}
	textGen := NewContentGenerator(mockProv, persona)

	mockTTS := &mockTTSServiceWithData{audioData: []byte("audio")}
	ttsConfig := &config.TTSConfig{Provider: "mock", Voice: "test"}

	audioGen := NewAudioContentGenerator(textGen, mockTTS, ttsConfig)

	_, err := audioGen.NextUserTurnAudio(
		context.Background(),
		[]types.Message{},
		"test-scenario",
		nil, // opts
	)

	if err == nil {
		t.Error("NextUserTurnAudio() expected error for empty text response")
	}
	if !strings.Contains(err.Error(), "no text content generated") {
		t.Errorf("NextUserTurnAudio() error = %v, want error containing 'no text content generated'", err)
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

func TestAudioResult_Fields(t *testing.T) {
	result := &AudioResult{
		TextResult: &pipeline.ExecutionResult{
			Response: &pipeline.Response{
				Content: "test content",
			},
		},
		Audio:       []byte("audio-data"),
		AudioFormat: tts.FormatPCM16,
	}

	if result.TextResult.Response.Content != "test content" {
		t.Error("AudioResult TextResult not set correctly")
	}
	if string(result.Audio) != "audio-data" {
		t.Error("AudioResult Audio not set correctly")
	}
	if result.AudioFormat.Name != "pcm" {
		t.Errorf("AudioResult AudioFormat = %s, want pcm", result.AudioFormat.Name)
	}
}
