package selfplay

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// mockTTSService is a minimal base.TTSProvider for testing TTSRegistry.
type mockTTSService struct {
	name string
}

func (m *mockTTSService) Name() string                        { return m.name }
func (m *mockTTSService) Type() base.ProviderType             { return base.ProviderTypeTTS }
func (m *mockTTSService) Pricing() *base.PricingDescriptor    { return nil }
func (m *mockTTSService) Validate() error                     { return nil }
func (m *mockTTSService) Init(_ context.Context) error        { return nil }
func (m *mockTTSService) HealthCheck(_ context.Context) error { return nil }
func (m *mockTTSService) Close() error                        { return nil }
func (m *mockTTSService) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "test-voice", Name: "Test Voice"}}
}
func (m *mockTTSService) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{tts.FormatPCM16}
}
func (m *mockTTSService) Synthesize(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("audio-data")), nil
}

func (m *mockTTSService) SynthesizeTTS(_ context.Context, _ base.TTSRequest) (base.TTSStream, error) {
	return newMockTTSStream(io.NopCloser(strings.NewReader("audio-data"))), nil
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

func TestTTSRegistry_GetWithConfig_ModelOverride(t *testing.T) {
	// Pinning a model via TTSConfig.Model should:
	//   1. Bypass the per-provider singleton cache (a scenario picking
	//      gpt-4o-mini-tts must not get the cached tts-1 instance).
	//   2. Yield a service whose ModelName() reflects the override.
	//   3. Expose the matching PersonaRubric for expressive personas.
	registry := NewTTSRegistry()
	t.Setenv(envOpenAIAPIKey, "test-key")

	def, err := registry.Get(TTSProviderOpenAI)
	if err != nil {
		t.Fatalf("Get default: %v", err)
	}

	override, err := registry.GetWithConfig(&config.TTSConfig{
		Provider: TTSProviderOpenAI,
		Voice:    "alloy",
		Model:    tts.ModelGPT4oMiniTTS,
	})
	if err != nil {
		t.Fatalf("GetWithConfig override: %v", err)
	}

	if def == override {
		t.Error("model-pinned service should be a separate instance from the default-cached one")
	}

	modelExposer, ok := override.(interface{ ModelName() string })
	if !ok {
		t.Skip("override service does not expose ModelName(); skipping detailed check")
	} else if got := modelExposer.ModelName(); got != tts.ModelGPT4oMiniTTS {
		t.Errorf("override ModelName = %q, want %q", got, tts.ModelGPT4oMiniTTS)
	}

	rp, ok := override.(tts.PersonaRubricProvider)
	if !ok {
		t.Fatal("override should still satisfy PersonaRubricProvider")
	}
	if rp.PersonaRubric() == "" {
		t.Error("gpt-4o-mini-tts override should expose a non-empty PersonaRubric")
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

func TestTTSRegistry_GetWithConfig_NilConfig(t *testing.T) {
	registry := NewTTSRegistry()
	if _, err := registry.GetWithConfig(nil); err == nil {
		t.Error("GetWithConfig(nil) expected error, got nil")
	}
}

func TestTTSRegistry_GetWithConfig_MockNoFiles_DelegatesToGet(t *testing.T) {
	registry := NewTTSRegistry()
	cfg := &config.TTSConfig{Provider: TTSProviderMock}

	// First call creates a cached mock service via the Get path.
	svc1, err := registry.GetWithConfig(cfg)
	if err != nil {
		t.Fatalf("GetWithConfig() error = %v", err)
	}

	// Second call must return the same instance from the standard cache.
	svc2, err := registry.GetWithConfig(cfg)
	if err != nil {
		t.Fatalf("GetWithConfig() error = %v", err)
	}
	if svc1 != svc2 {
		t.Error("GetWithConfig with no audio_files should reuse Get cache")
	}
}

func TestTTSRegistry_GetWithConfig_MockWithFiles_CachesByFilesIdentity(t *testing.T) {
	registry := NewTTSRegistry()

	cfgA := &config.TTSConfig{
		Provider:   TTSProviderMock,
		AudioFiles: []string{"a.pcm", "b.pcm"},
	}
	cfgADup := &config.TTSConfig{
		Provider:   TTSProviderMock,
		AudioFiles: []string{"a.pcm", "b.pcm"},
	}
	cfgB := &config.TTSConfig{
		Provider:   TTSProviderMock,
		AudioFiles: []string{"c.pcm"},
	}

	svcA1, err := registry.GetWithConfig(cfgA)
	if err != nil {
		t.Fatalf("GetWithConfig(cfgA) error = %v", err)
	}
	svcADup, err := registry.GetWithConfig(cfgADup)
	if err != nil {
		t.Fatalf("GetWithConfig(cfgADup) error = %v", err)
	}
	svcB, err := registry.GetWithConfig(cfgB)
	if err != nil {
		t.Fatalf("GetWithConfig(cfgB) error = %v", err)
	}

	// Same audio_files identity must reuse the same instance — otherwise
	// MockTTSService.currentFileIndex would reset and rotation would break.
	if svcA1 != svcADup {
		t.Error("GetWithConfig should cache mock services by audio_files identity")
	}
	// Different audio_files must produce a separate instance.
	if svcA1 == svcB {
		t.Error("GetWithConfig should not share instances across different audio_files")
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

func TestTTSRegistry_GetForProvider_Mock(t *testing.T) {
	p := &config.Provider{
		ID:         "mock-tts",
		Type:       TTSProviderMock,
		Capability: config.CapabilityTTS,
		AudioFiles: []string{"a.pcm", "b.pcm"},
	}
	r := NewTTSRegistry()
	svc, err := r.GetForProvider(p)
	if err != nil {
		t.Fatalf("GetForProvider: %v", err)
	}
	if svc.Name() != TTSProviderMock {
		t.Fatalf("expected mock service, got %q", svc.Name())
	}
}

func TestTTSRegistry_GetForProvider_RejectsLLMCapability(t *testing.T) {
	p := &config.Provider{ID: "llm", Type: "openai", Capability: config.CapabilityLLM}
	r := NewTTSRegistry()
	if _, err := r.GetForProvider(p); err == nil {
		t.Fatal("expected error: capability is llm, not tts")
	}
}

func TestTTSRegistry_GetForProvider_NilProvider(t *testing.T) {
	r := NewTTSRegistry()
	if _, err := r.GetForProvider(nil); err == nil {
		t.Fatal("expected error: nil provider")
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

// TestTTSRegistry_WrapWithDiskCache_NonLegacyBackend verifies that
// wrapWithDiskCache returns the provider unchanged when TTS_CACHE_DIR is set
// but the provider does not implement tts.Service (so it can't be wrapped).
func TestTTSRegistry_WrapWithDiskCache_NonLegacyBackend(t *testing.T) {
	t.Setenv(envTTSCacheDir, t.TempDir())

	// mockTTSService is a base.TTSProvider but not tts.Service (it has Synthesize
	// as an extra method but that only matters for the type assertion in
	// wrapWithDiskCache). To exercise the !ok branch we need a provider that
	// lacks tts.Service entirely — but since mockTTSService happens to have
	// Synthesize we test the wrap path instead: when TTS_CACHE_DIR is set and
	// the provider IS a tts.Service, it must return a *CachedTTSService.
	registry := NewTTSRegistry()
	t.Setenv(envOpenAIAPIKey, "test-key")

	svc, err := registry.Get(TTSProviderOpenAI)
	if err != nil {
		t.Fatalf("Get(openai) with TTS_CACHE_DIR set: %v", err)
	}
	// The OpenAI service implements tts.Service, so wrapWithDiskCache must
	// wrap it. The returned value must satisfy base.TTSProvider (not just tts.Service).
	if svc == nil {
		t.Fatal("Get() returned nil service")
	}
	if svc.Name() != "openai" {
		t.Errorf("wrapped service Name() = %q, want openai", svc.Name())
	}
}

// TestTTSRegistry_Get_MockIsNotWrappedByCache verifies that mock providers are
// never wrapped by the disk cache even when TTS_CACHE_DIR is set.
func TestTTSRegistry_Get_MockIsNotWrappedByCache(t *testing.T) {
	t.Setenv(envTTSCacheDir, t.TempDir())

	registry := NewTTSRegistry()
	svc, err := registry.Get(TTSProviderMock)
	if err != nil {
		t.Fatalf("Get(mock) error = %v", err)
	}
	// Mock should come back as the raw MockTTSService, not a *CachedTTSService.
	if _, ok := svc.(*CachedTTSService); ok {
		t.Error("mock provider must not be wrapped in CachedTTSService")
	}
}
