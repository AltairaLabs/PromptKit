package base

import "net/http"

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
