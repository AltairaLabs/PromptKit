package tts

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterFactory("elevenlabs", func(spec ProviderSpec) (Service, error) {
		opts := []ElevenLabsOption{}
		if spec.Model != "" {
			opts = append(opts, WithElevenLabsModel(spec.Model))
		}
		if spec.BaseURL != "" {
			opts = append(opts, WithElevenLabsBaseURL(spec.BaseURL))
		}
		return NewElevenLabs(APIKeyFromCredential(spec.Credential), opts...), nil
	})
}
