package tts

import "github.com/AltairaLabs/PromptKit/runtime/providers/base"

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
		return svc, nil
	})
}
