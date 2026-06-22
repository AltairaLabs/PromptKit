package selfplay

import (
	"fmt"
	"os"
	"sync"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
)

const (
	sttProviderOpenAI = "openai"
	envOpenAISTTKey   = "OPENAI_API_KEY"
)

// STTRegistry manages STT service instances by provider type, mirroring
// TTSRegistry. Used by the interactive voice console's VAD branch to transcribe
// microphone audio.
type STTRegistry struct {
	mu        sync.RWMutex
	preloaded map[string]stt.Service
}

// NewSTTRegistry creates a new STT registry.
func NewSTTRegistry() *STTRegistry { return &STTRegistry{} }

// Register adds a pre-configured STT service keyed by provider type, mirroring
// TTSRegistry.Register. A registered service takes precedence over the built-in
// vendor adapters in GetForProvider, which lets tests inject a deterministic
// fake without requiring an API key.
func (r *STTRegistry) Register(providerType string, svc stt.Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.preloaded == nil {
		r.preloaded = make(map[string]stt.Service)
	}
	r.preloaded[providerType] = svc
}

// GetForProvider returns an stt.Service configured from a loaded STT provider
// yaml. Validates role == "stt". A service registered via Register for the
// provider Type wins; otherwise routing is by Type, pinning Model when set.
func (r *STTRegistry) GetForProvider(p *config.Provider) (stt.Service, error) {
	if p == nil {
		return nil, fmt.Errorf("nil STT provider")
	}
	if p.GetRole() != config.RoleSTT {
		return nil, fmt.Errorf("provider %s has role %q, expected stt", p.ID, p.GetRole())
	}
	r.mu.RLock()
	if svc, ok := r.preloaded[p.Type]; ok {
		r.mu.RUnlock()
		return svc, nil
	}
	r.mu.RUnlock()
	switch p.Type {
	case sttProviderOpenAI:
		return r.createOpenAI(p.Model)
	default:
		return nil, fmt.Errorf("unsupported STT provider: %s (supported: %s)", p.Type, sttProviderOpenAI)
	}
}

// createOpenAI creates an OpenAI STT service. When model is empty, the adapter's
// default (whisper-1) applies; pass a specific model to override.
// stt.OpenAIOption is a type alias for base.HTTPServiceOption, so base.WithModel
// is accepted directly.
func (r *STTRegistry) createOpenAI(model string) (stt.Service, error) {
	apiKey := os.Getenv(envOpenAISTTKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openAI STT requires %s environment variable", envOpenAISTTKey)
	}
	opts := []base.HTTPServiceOption{}
	if model != "" {
		opts = append(opts, base.WithModel(model))
	}
	return stt.NewOpenAI(apiKey, opts...), nil
}
