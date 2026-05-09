package tts

import "github.com/AltairaLabs/PromptKit/runtime/providers/base"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterFactory("cartesia", func(spec ProviderSpec) (Service, error) {
		opts := []CartesiaOption{}
		if spec.Model != "" {
			opts = append(opts, base.WithModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, base.WithBaseURL(spec.BaseURL))
		}
		svc := NewCartesia(APIKeyFromCredential(spec.Credential), opts...)
		if v, ok := spec.AdditionalConfig["ws_url"].(string); ok && v != "" {
			WithCartesiaWSURL(v)(svc)
		}
		if p := PricingFromSpec(spec); p != nil {
			WithCartesiaPricing(p)(svc)
		}
		return svc, nil
	})
}
