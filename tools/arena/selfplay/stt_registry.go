package selfplay

import (
	"fmt"
	"os"

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
type STTRegistry struct{}

// NewSTTRegistry creates a new STT registry.
func NewSTTRegistry() *STTRegistry { return &STTRegistry{} }

// GetForProvider returns an stt.Service configured from a loaded STT provider
// yaml. Validates role == "stt". Routes by Type; pins Model when set.
func (r *STTRegistry) GetForProvider(p *config.Provider) (stt.Service, error) {
	if p == nil {
		return nil, fmt.Errorf("nil STT provider")
	}
	if p.GetRole() != config.RoleSTT {
		return nil, fmt.Errorf("provider %s has role %q, expected stt", p.ID, p.GetRole())
	}
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
