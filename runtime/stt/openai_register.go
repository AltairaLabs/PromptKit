package stt

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterPlatforms("openai", base.PlatformAzure)
	RegisterFactory("openai", func(spec ProviderSpec) (Service, error) {
		opts := []OpenAIOption{}
		if spec.Model != "" {
			opts = append(opts, base.WithModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, base.WithBaseURL(spec.BaseURL))
		}
		authOpts, apiKey, err := base.OpenAIWireAuth(&spec, defaultOpenAITimeout)
		if err != nil {
			return nil, err
		}
		svc := NewOpenAI(apiKey, append(opts, authOpts...)...)
		// Apply YAML-defined pricing override when present in additional_config.
		if p := PricingFromSpec(spec); p != nil {
			svc.SetPricing(p)
		}
		return svc, nil
	})
}
