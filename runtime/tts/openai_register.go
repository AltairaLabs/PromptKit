package tts

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
		if p := PricingFromSpec(spec); p != nil {
			opts = append(opts, WithOpenAIPricing(p))
		}
		return NewOpenAI(APIKeyFromCredential(spec.Credential), opts...), nil
	})
}
