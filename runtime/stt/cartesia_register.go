package stt

//nolint:gochecknoinits // Factory registration requires init
func init() {
	registerAPIKeyFactory("cartesia", func(apiKey string, opts ...CartesiaOption) pricedService {
		return NewCartesia(apiKey, opts...)
	})
}
