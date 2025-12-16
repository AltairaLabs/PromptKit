package selfplay

import (
	"fmt"
	"os"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// Supported TTS provider names.
const (
	TTSProviderOpenAI     = "openai"
	TTSProviderElevenLabs = "elevenlabs"
	TTSProviderCartesia   = "cartesia"
)

// Environment variable names for TTS API keys.
//
//nolint:gosec // These are env var names, not credentials
const (
	envOpenAIAPIKey     = "OPENAI_API_KEY"
	envElevenLabsAPIKey = "ELEVENLABS_API_KEY"
	envCartesiaAPIKey   = "CARTESIA_API_KEY"
)

// TTSRegistry manages TTS service instances by provider name.
// It supports lazy initialization and caching of TTS services.
type TTSRegistry struct {
	services map[string]tts.Service
	mu       sync.RWMutex
}

// NewTTSRegistry creates a new TTS registry.
func NewTTSRegistry() *TTSRegistry {
	return &TTSRegistry{
		services: make(map[string]tts.Service),
	}
}

// Get returns a TTS service for the given provider name.
// Services are lazily initialized on first request and cached.
func (r *TTSRegistry) Get(provider string) (tts.Service, error) {
	// Check cache first
	r.mu.RLock()
	if svc, exists := r.services[provider]; exists {
		r.mu.RUnlock()
		return svc, nil
	}
	r.mu.RUnlock()

	// Create service
	svc, err := r.createService(provider)
	if err != nil {
		return nil, err
	}

	// Cache and return
	r.mu.Lock()
	r.services[provider] = svc
	r.mu.Unlock()

	return svc, nil
}

// Register adds a pre-configured TTS service to the registry.
// This is useful for testing or when using custom configurations.
func (r *TTSRegistry) Register(provider string, svc tts.Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[provider] = svc
}

// createService creates a new TTS service for the given provider.
func (r *TTSRegistry) createService(provider string) (tts.Service, error) {
	switch provider {
	case TTSProviderOpenAI:
		return r.createOpenAI()
	case TTSProviderElevenLabs:
		return r.createElevenLabs()
	case TTSProviderCartesia:
		return r.createCartesia()
	default:
		return nil, fmt.Errorf("unsupported TTS provider: %s (supported: %s, %s, %s)",
			provider, TTSProviderOpenAI, TTSProviderElevenLabs, TTSProviderCartesia)
	}
}

// createOpenAI creates an OpenAI TTS service.
func (r *TTSRegistry) createOpenAI() (tts.Service, error) {
	apiKey := os.Getenv(envOpenAIAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openAI TTS requires %s environment variable", envOpenAIAPIKey)
	}
	return tts.NewOpenAI(apiKey), nil
}

// createElevenLabs creates an ElevenLabs TTS service.
func (r *TTSRegistry) createElevenLabs() (tts.Service, error) {
	apiKey := os.Getenv(envElevenLabsAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("elevenLabs TTS requires %s environment variable", envElevenLabsAPIKey)
	}
	return tts.NewElevenLabs(apiKey), nil
}

// createCartesia creates a Cartesia TTS service.
func (r *TTSRegistry) createCartesia() (tts.Service, error) {
	apiKey := os.Getenv(envCartesiaAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("cartesia TTS requires %s environment variable", envCartesiaAPIKey)
	}
	return tts.NewCartesia(apiKey), nil
}

// SupportedProviders returns a list of supported TTS provider names.
func (r *TTSRegistry) SupportedProviders() []string {
	return []string{TTSProviderOpenAI, TTSProviderElevenLabs, TTSProviderCartesia}
}

// Clear removes all cached services.
func (r *TTSRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services = make(map[string]tts.Service)
}
