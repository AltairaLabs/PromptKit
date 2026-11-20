package stt

//nolint:gochecknoinits // Factory registration requires init
func init() {
	registerAPIKeyFactory("elevenlabs", func(apiKey string, opts ...ElevenLabsOption) pricedService {
		return NewElevenLabs(apiKey, opts...)
	})
}
