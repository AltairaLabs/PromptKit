package tts

import "github.com/AltairaLabs/PromptKit/runtime/providers/base"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterFactory("openai", func(spec ProviderSpec) (Service, error) {
		opts := []OpenAIOption{}
		if spec.Model != "" {
			opts = append(opts, base.WithModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, base.WithBaseURL(spec.BaseURL))
		}
		svc := NewOpenAI(APIKeyFromCredential(spec.Credential), opts...)
		if p := PricingFromSpec(spec); p != nil {
			WithOpenAIPricing(p)(svc)
		}
		return svc, nil
	})
}
