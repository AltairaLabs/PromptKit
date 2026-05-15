package selfplay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// Supported TTS provider names.
const (
	TTSProviderOpenAI     = "openai"
	TTSProviderElevenLabs = "elevenlabs"
	TTSProviderCartesia   = "cartesia"
	// TTSProviderMock is defined in mock_tts.go
)

// Environment variable names for TTS API keys.
//
//nolint:gosec // These are env var names, not credentials
const (
	envOpenAIAPIKey      = "OPENAI_API_KEY"
	envElevenLabsAPIKey  = "ELEVENLABS_API_KEY"
	envCartesiaAPIKey    = "CARTESIA_API_KEY"
	envMockTTSAudioDir   = "MOCK_TTS_AUDIO_DIR"   // Directory containing .pcm files
	envMockTTSAudioFiles = "MOCK_TTS_AUDIO_FILES" // Comma-separated list of .pcm files
)

// TTSRegistry manages TTS provider instances by provider name.
// It supports lazy initialization and caching of TTS providers.
type TTSRegistry struct {
	services    map[string]base.TTSProvider
	mockByFiles map[string]base.TTSProvider
	mu          sync.RWMutex
}

// NewTTSRegistry creates a new TTS registry.
func NewTTSRegistry() *TTSRegistry {
	return &TTSRegistry{
		services:    make(map[string]base.TTSProvider),
		mockByFiles: make(map[string]base.TTSProvider),
	}
}

// Get returns a TTS provider for the given provider name.
// Providers are lazily initialized on first request and cached.
//
// When the TTS_CACHE_DIR environment variable is set, the returned
// provider is wrapped in a CachedTTSService rooted at that directory so
// repeated synthesis of the same text doesn't re-bill the upstream
// provider. Mock providers are exempt — they're already deterministic
// and would just bloat the cache.
func (r *TTSRegistry) Get(provider string) (base.TTSProvider, error) {
	// Check cache first
	r.mu.RLock()
	if svc, exists := r.services[provider]; exists {
		r.mu.RUnlock()
		return svc, nil
	}
	r.mu.RUnlock()

	// Create provider
	svc, err := r.createService(provider)
	if err != nil {
		return nil, err
	}
	svc = wrapWithDiskCache(provider, svc)

	// Cache and return
	r.mu.Lock()
	r.services[provider] = svc
	r.mu.Unlock()

	return svc, nil
}

// wrapWithDiskCache wraps svc in a CachedTTSService when TTS_CACHE_DIR is
// set, leaves it untouched otherwise. Mock providers always pass through —
// they're already free and deterministic.
func wrapWithDiskCache(provider string, svc base.TTSProvider) base.TTSProvider {
	if provider == TTSProviderMock {
		return svc
	}
	dir := resolveTTSCacheDir()
	if dir == "" {
		return svc
	}
	// NewCachedTTSService expects a tts.Service; type-assert so we can wrap
	// providers that also satisfy the legacy interface (all three real impls do).
	legacySvc, ok := svc.(tts.Service)
	if !ok {
		return svc
	}
	wrapped, err := NewCachedTTSService(legacySvc, dir)
	if err != nil {
		// Cache directory unavailable — log and fall back to the bare
		// backend so synthesis still works. Tests that depend on caching
		// will catch the regression themselves.
		return svc
	}
	// CachedTTSService satisfies base.TTSProvider — the type assertion is safe.
	return wrapped.(base.TTSProvider)
}

// GetForProvider returns a base.TTSProvider configured from a loaded TTS
// provider yaml. Provider routing is by Type (cartesia/elevenlabs/openai/mock);
// the voice/sample_rate/audio_files/model fields on the provider populate
// the synthesis config.
//
// Validates that the provider has Capability == "tts" — guards against
// callers passing an LLM provider by mistake.
func (r *TTSRegistry) GetForProvider(p *config.Provider) (base.TTSProvider, error) {
	if p == nil {
		return nil, fmt.Errorf("nil TTS provider")
	}
	if p.GetCapability() != config.CapabilityTTS {
		return nil, fmt.Errorf("provider %s has capability %q, expected tts",
			p.ID, p.GetCapability())
	}

	// For mock provider with audio files, cache by file-list identity
	// so concurrent runs share one MockTTSService and rotation works.
	if p.Type == TTSProviderMock && len(p.AudioFiles) > 0 {
		key := strings.Join(p.AudioFiles, "|")
		r.mu.RLock()
		if svc, exists := r.mockByFiles[key]; exists {
			r.mu.RUnlock()
			return svc, nil
		}
		r.mu.RUnlock()
		r.mu.Lock()
		defer r.mu.Unlock()
		if svc, exists := r.mockByFiles[key]; exists {
			return svc, nil
		}
		svc := NewMockTTSWithFiles(p.AudioFiles)
		r.mockByFiles[key] = svc
		return svc, nil
	}

	// Real-vendor TTS: pin model if set on the provider yaml.
	if p.Model != "" {
		svc, err := r.createServiceWithModel(p.Type, p.Model)
		if err != nil {
			return nil, err
		}
		return wrapWithDiskCache(p.Type, svc), nil
	}

	return r.Get(p.Type)
}

// Register adds a pre-configured TTS provider to the registry.
// This is useful for testing or when using custom configurations.
func (r *TTSRegistry) Register(provider string, svc base.TTSProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[provider] = svc
}

// createService creates a new TTS provider for the given provider name
// using the adapter's default model.
func (r *TTSRegistry) createService(provider string) (base.TTSProvider, error) {
	return r.createServiceWithModel(provider, "")
}

// createServiceWithModel creates a new TTS provider, optionally pinning a
// specific model. Empty model = adapter default. Used by GetForProvider
// when a provider yaml pins a specific model.
func (r *TTSRegistry) createServiceWithModel(provider, model string) (base.TTSProvider, error) {
	switch provider {
	case TTSProviderOpenAI:
		return r.createOpenAI(model)
	case TTSProviderElevenLabs:
		return r.createElevenLabs(model)
	case TTSProviderCartesia:
		return r.createCartesia(model)
	case TTSProviderMock:
		return r.createMock()
	default:
		return nil, fmt.Errorf("unsupported TTS provider: %s (supported: %s, %s, %s, %s)",
			provider, TTSProviderOpenAI, TTSProviderElevenLabs, TTSProviderCartesia, TTSProviderMock)
	}
}

// createMock creates a mock TTS provider, optionally configured with audio files.
func (r *TTSRegistry) createMock() (base.TTSProvider, error) {
	var audioFiles []string

	// Check for explicit file list first
	if files := os.Getenv(envMockTTSAudioFiles); files != "" {
		audioFiles = strings.Split(files, ",")
		for i, f := range audioFiles {
			audioFiles[i] = strings.TrimSpace(f)
		}
	} else if dir := os.Getenv(envMockTTSAudioDir); dir != "" {
		// Load all .pcm files from the directory
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".pcm") {
					audioFiles = append(audioFiles, filepath.Join(dir, entry.Name()))
				}
			}
		}
	}

	if len(audioFiles) > 0 {
		return NewMockTTSWithFiles(audioFiles), nil
	}
	return NewMockTTS(), nil
}

// createOpenAI creates an OpenAI TTS provider. When model is empty, the
// adapter's default (tts-1) applies; pass "gpt-4o-mini-tts" to unlock the
// expressive instructions field and the persona characterization rubric.
func (r *TTSRegistry) createOpenAI(model string) (base.TTSProvider, error) {
	apiKey := os.Getenv(envOpenAIAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openAI TTS requires %s environment variable", envOpenAIAPIKey)
	}
	opts := []base.HTTPServiceOption{}
	if model != "" {
		opts = append(opts, base.WithModel(model))
	}
	return tts.NewOpenAI(apiKey, opts...), nil
}

// createElevenLabs creates an ElevenLabs TTS provider.
//
// Selfplay defaults to turbo_v2_5 because it's optimized for real-time
// conversational agents (~250ms TTFB vs ~1s for multilingual_v2). The
// default multilingual_v2 model would dominate per-turn latency. Pass
// "eleven_v3" (or any "eleven_v3*") to unlock inline characterization
// tags and the persona rubric.
func (r *TTSRegistry) createElevenLabs(model string) (base.TTSProvider, error) {
	apiKey := os.Getenv(envElevenLabsAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("elevenLabs TTS requires %s environment variable", envElevenLabsAPIKey)
	}
	if model == "" {
		model = tts.ElevenLabsModelTurbo
	}
	return tts.NewElevenLabs(apiKey, base.WithModel(model)), nil
}

// createCartesia creates a Cartesia TTS provider. Empty model = adapter
// default (sonic-3); Cartesia's PersonaRubric returns the emotion-only
// rubric regardless of model, so no model override is needed to unlock
// expressive personas on Cartesia.
func (r *TTSRegistry) createCartesia(model string) (base.TTSProvider, error) {
	apiKey := os.Getenv(envCartesiaAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("cartesia TTS requires %s environment variable", envCartesiaAPIKey)
	}
	opts := []base.HTTPServiceOption{}
	if model != "" {
		opts = append(opts, base.WithModel(model))
	}
	return tts.NewCartesia(apiKey, opts...), nil
}

// SupportedProviders returns a list of supported TTS provider names.
func (r *TTSRegistry) SupportedProviders() []string {
	return []string{TTSProviderOpenAI, TTSProviderElevenLabs, TTSProviderCartesia, TTSProviderMock}
}

// Clear removes all cached services.
func (r *TTSRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services = make(map[string]base.TTSProvider)
}
