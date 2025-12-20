package selfplay

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// mockTTSService is a mock TTS service for testing.
type mockTTSService struct {
	name string
}

func (m *mockTTSService) Name() string {
	return m.name
}

func (m *mockTTSService) Synthesize(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("audio-data")), nil
}

func (m *mockTTSService) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "test-voice", Name: "Test Voice"}}
}

func (m *mockTTSService) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{tts.FormatPCM16}
}

func TestTTSRegistry_Register(t *testing.T) {
	registry := NewTTSRegistry()
	mockSvc := &mockTTSService{name: "mock"}

	registry.Register("mock", mockSvc)

	svc, err := registry.Get("mock")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if svc.Name() != "mock" {
		t.Errorf("Get() returned service with name %q, want %q", svc.Name(), "mock")
	}
}

func TestTTSRegistry_Get_UnsupportedProvider(t *testing.T) {
	registry := NewTTSRegistry()

	_, err := registry.Get("unsupported")
	if err == nil {
		t.Error("Get() expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported TTS provider") {
		t.Errorf("Get() error = %v, want error containing 'unsupported TTS provider'", err)
	}
}

func TestTTSRegistry_Get_MissingAPIKey(t *testing.T) {
	registry := NewTTSRegistry()

	// OpenAI without API key
	t.Setenv(envOpenAIAPIKey, "")
	_, err := registry.Get(TTSProviderOpenAI)
	if err == nil {
		t.Error("Get(openai) expected error for missing API key")
	}

	// ElevenLabs without API key
	t.Setenv(envElevenLabsAPIKey, "")
	_, err = registry.Get(TTSProviderElevenLabs)
	if err == nil {
		t.Error("Get(elevenlabs) expected error for missing API key")
	}

	// Cartesia without API key
	t.Setenv(envCartesiaAPIKey, "")
	_, err = registry.Get(TTSProviderCartesia)
	if err == nil {
		t.Error("Get(cartesia) expected error for missing API key")
	}
}

func TestTTSRegistry_Get_WithAPIKey(t *testing.T) {
	registry := NewTTSRegistry()

	// Set test API keys
	t.Setenv(envOpenAIAPIKey, "test-openai-key")
	t.Setenv(envElevenLabsAPIKey, "test-elevenlabs-key")
	t.Setenv(envCartesiaAPIKey, "test-cartesia-key")

	tests := []struct {
		provider string
		wantName string
	}{
		{TTSProviderOpenAI, "openai"},
		{TTSProviderElevenLabs, "elevenlabs"},
		{TTSProviderCartesia, "cartesia"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			svc, err := registry.Get(tt.provider)
			if err != nil {
				t.Fatalf("Get(%s) error = %v", tt.provider, err)
			}
			if svc.Name() != tt.wantName {
				t.Errorf("Get(%s) returned service name %q, want %q", tt.provider, svc.Name(), tt.wantName)
			}
		})
	}
}

func TestTTSRegistry_Get_Caching(t *testing.T) {
	registry := NewTTSRegistry()
	t.Setenv(envOpenAIAPIKey, "test-key")

	// Get twice
	svc1, err := registry.Get(TTSProviderOpenAI)
	if err != nil {
		t.Fatalf("First Get() error = %v", err)
	}

	svc2, err := registry.Get(TTSProviderOpenAI)
	if err != nil {
		t.Fatalf("Second Get() error = %v", err)
	}

	// Should return the same instance
	if svc1 != svc2 {
		t.Error("Get() should return cached service instance")
	}
}

func TestTTSRegistry_SupportedProviders(t *testing.T) {
	registry := NewTTSRegistry()

	providers := registry.SupportedProviders()
	if len(providers) != 4 {
		t.Errorf("SupportedProviders() returned %d providers, want 4", len(providers))
	}

	expected := map[string]bool{
		TTSProviderOpenAI:     true,
		TTSProviderElevenLabs: true,
		TTSProviderCartesia:   true,
		TTSProviderMock:       true,
	}

	for _, p := range providers {
		if !expected[p] {
			t.Errorf("SupportedProviders() returned unexpected provider %q", p)
		}
	}
}

func TestTTSRegistry_Clear(t *testing.T) {
	registry := NewTTSRegistry()
	customSvc := &mockTTSService{name: "custom-provider"}

	registry.Register("custom", customSvc)

	// Verify it's registered
	_, err := registry.Get("custom")
	if err != nil {
		t.Fatalf("Get() before Clear() error = %v", err)
	}

	// Clear the registry
	registry.Clear()

	// Now it should fail ("custom" is not a supported provider for createService)
	_, err = registry.Get("custom")
	if err == nil {
		t.Error("Get() after Clear() expected error")
	}
}

func TestTTSRegistry_Get_MockProvider(t *testing.T) {
	registry := NewTTSRegistry()

	// Mock provider should work without any API key
	svc, err := registry.Get(TTSProviderMock)
	if err != nil {
		t.Fatalf("Get(mock) error = %v", err)
	}
	if svc.Name() != TTSProviderMock {
		t.Errorf("Get(mock) returned service name %q, want %q", svc.Name(), TTSProviderMock)
	}
}

func TestMockTTS_Synthesize(t *testing.T) {
	mock := NewMockTTS()

	// Test synthesis
	reader, err := mock.Synthesize(context.Background(), "Hello world", tts.SynthesisConfig{})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	defer reader.Close()

	// Read the audio data
	audio, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	// Should have generated audio (minimum 4800 samples * 2 bytes = 9600 bytes)
	if len(audio) < 9600 {
		t.Errorf("Synthesize() generated %d bytes, want at least 9600", len(audio))
	}
}

func TestMockTTS_SupportedVoices(t *testing.T) {
	mock := NewMockTTS()
	voices := mock.SupportedVoices()

	if len(voices) == 0 {
		t.Error("SupportedVoices() returned empty list")
	}
}

func TestMockTTS_SupportedFormats(t *testing.T) {
	mock := NewMockTTS()
	formats := mock.SupportedFormats()

	if len(formats) == 0 {
		t.Error("SupportedFormats() returned empty list")
	}
}
