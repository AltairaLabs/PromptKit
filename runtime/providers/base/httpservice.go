package base

import (
	"net/http"
	"time"
)

// HTTPServiceDefaults carries the construction-time defaults for an HTTP
// service provider (TTS, STT impls). Used by NewHTTPService to produce the
// standard *Implementation + *HTTPServiceFields pair every impl embeds.
type HTTPServiceDefaults struct {
	// Name is the provider's unique registry name (e.g. "openai-whisper").
	Name string
	// Type is the capability discriminator (inference, tts, stt, ...).
	Type ProviderType
	// Pricing is the compiled-in pricing descriptor.
	Pricing *PricingDescriptor
	// BaseURL is the default API endpoint.
	BaseURL string
	// Model is the default model identifier.
	Model string
	// Timeout is the default HTTP client timeout. Zero is treated as
	// http.DefaultClient (no timeout) — pass a positive value.
	Timeout time.Duration
}

// NewHTTPService produces the standard *Implementation + *HTTPServiceFields
// pair every HTTP-backed provider impl embeds, applying any caller-supplied
// HTTPServiceOption mutations on the way out. Eliminates per-impl
// constructor boilerplate.
//
//nolint:gocritic // hugeParam: defaults passed by value to keep call sites composable.
func NewHTTPService(
	apiKey string,
	defaults HTTPServiceDefaults,
	opts ...HTTPServiceOption,
) (*Implementation, *HTTPServiceFields) {
	impl := NewImplementation(defaults.Name, defaults.Type, defaults.Pricing)
	fields := &HTTPServiceFields{
		APIKey:  apiKey,
		BaseURL: defaults.BaseURL,
		Model:   defaults.Model,
		Client:  &http.Client{Timeout: defaults.Timeout},
	}
	for _, opt := range opts {
		opt(fields)
	}
	return impl, fields
}

// HTTPServiceFields is the shared HTTP-call configuration embedded by
// service-style providers (TTS, STT impls). Each impl embeds *HTTPServiceFields
// to inherit APIKey/BaseURL/Model/Client storage plus the With* option helpers
// in this package — eliminating per-package With* boilerplate.
type HTTPServiceFields struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

// HTTPServiceOption mutates an HTTPServiceFields. Each impl re-types this for
// their own option signatures (e.g., type OpenAIOption = HTTPServiceOption).
type HTTPServiceOption func(*HTTPServiceFields)

// WithBaseURL overrides the service's API base URL (testing, proxies).
func WithBaseURL(url string) HTTPServiceOption {
	return func(f *HTTPServiceFields) { f.BaseURL = url }
}

// WithClient overrides the HTTP client (custom timeout / transport).
func WithClient(c *http.Client) HTTPServiceOption {
	return func(f *HTTPServiceFields) { f.Client = c }
}

// WithModel overrides the model name.
func WithModel(m string) HTTPServiceOption {
	return func(f *HTTPServiceFields) { f.Model = m }
}

// WithAPIKey sets the API key (rarely used directly; constructors take it as a
// positional arg, but exposed for completeness).
func WithAPIKey(k string) HTTPServiceOption {
	return func(f *HTTPServiceFields) { f.APIKey = k }
}
