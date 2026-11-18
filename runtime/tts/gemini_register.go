package tts

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// platformVertex is the PlatformConfig.Type of a Vertex AI-hosted provider.
const platformVertex = "vertex"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterPlatforms(providerGemini, platformVertex)
	RegisterFactory(providerGemini, newGeminiFromSpec)
}

// newGeminiFromSpec builds a Gemini TTS service from a provider spec: on
// Vertex AI when the spec keeps its platform (Create leaves only a declared
// one, vertex), on the Gemini API with the spec's API key otherwise.
func newGeminiFromSpec(spec ProviderSpec) (Service, error) {
	opts := []GeminiOption{}
	if spec.Model != "" {
		opts = append(opts, base.WithModel(spec.Model))
	}
	if spec.BaseURL != "" {
		opts = append(opts, base.WithBaseURL(spec.BaseURL))
	}
	var svc *GeminiService
	if pc := spec.Platform; pc != nil {
		if pc.Project == "" || pc.Region == "" {
			return nil, fmt.Errorf("tts %q on vertex needs platform.project and platform.region", spec.ID)
		}
		if spec.Credential == nil {
			return nil, fmt.Errorf("tts %q on vertex needs a platform credential", spec.ID)
		}
		svc = NewGeminiOnVertex(spec.Credential, pc.Project, pc.Region, opts...)
	} else {
		svc = NewGemini(APIKeyFromCredential(spec.Credential), opts...)
	}
	if p := PricingFromSpec(spec); p != nil {
		svc.SetPricing(p)
	}
	return svc, nil
}
