package tts

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterFactory("elevenlabs", func(spec ProviderSpec) (Service, error) {
		opts := []ElevenLabsOption{}
		if spec.Model != "" {
			opts = append(opts, base.WithModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, base.WithBaseURL(spec.BaseURL))
		}
		svc := NewElevenLabs(APIKeyFromCredential(spec.Credential), opts...)
		if p := PricingFromSpec(spec); p != nil {
			svc.SetPricing(p)
		}
		if raw, set := spec.AdditionalConfig["inline_tags"]; set {
			on, ok := raw.(bool)
			if !ok {
				return nil, fmt.Errorf("elevenlabs tts provider %q: additional_config.inline_tags must be true or false, got %q",
					spec.ID, fmt.Sprint(raw))
			}
			svc.SetInlineTags(on)
		}
		return svc, nil
	})
}
