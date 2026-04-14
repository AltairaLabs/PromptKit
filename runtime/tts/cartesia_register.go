package tts

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterFactory("cartesia", func(spec ProviderSpec) (Service, error) {
		opts := []CartesiaOption{}
		if spec.Model != "" {
			opts = append(opts, WithCartesiaModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, WithCartesiaBaseURL(spec.BaseURL))
		}
		if v, ok := spec.AdditionalConfig["ws_url"].(string); ok && v != "" {
			opts = append(opts, WithCartesiaWSURL(v))
		}
		return NewCartesia(APIKeyFromCredential(spec.Credential), opts...), nil
	})
}
