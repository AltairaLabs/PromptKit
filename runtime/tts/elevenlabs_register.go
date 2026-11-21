package tts

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
			if on, ok := raw.(bool); ok {
				svc.SetInlineTags(on)
			} else {
				logger.Warn("elevenlabs tts: ignoring non-boolean additional_config.inline_tags; "+
					"the model ID decides", "provider", spec.ID, "value", fmt.Sprint(raw), "expected", "true|false")
			}
		}
		return svc, nil
	})
}
