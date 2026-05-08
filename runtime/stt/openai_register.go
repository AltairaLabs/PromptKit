package stt

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterFactory("openai", func(spec ProviderSpec) (Service, error) {
		opts := []OpenAIOption{}
		if spec.Model != "" {
			opts = append(opts, WithOpenAIModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, WithOpenAIBaseURL(spec.BaseURL))
		}
		// Apply YAML-defined pricing override when present in additional_config.
		if p := PricingFromSpec(spec); p != nil {
			opts = append(opts, WithOpenAIPricing(p))
		}
		return NewOpenAI(APIKeyFromCredential(spec.Credential), opts...), nil
	})
}
