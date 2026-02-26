// Package httputil provides shared HTTP client construction utilities
// for the PromptKit project. It centralizes timeout defaults and client
// creation so that every module uses consistent configuration.
package httputil

import (
	"net/http"
	"time"
)

// Standard timeout defaults used across the project.
const (
	// DefaultProviderTimeout is the HTTP timeout for LLM provider calls
	// (e.g. OpenAI, Claude, Gemini). Provider requests can involve large
	// payloads and long inference times, so they use a longer timeout.
	DefaultProviderTimeout = 60 * time.Second

	// DefaultToolTimeout is the HTTP timeout for tool / webhook calls
	// made by the SDK HTTP executor. These are typically shorter-lived
	// API requests.
	DefaultToolTimeout = 30 * time.Second
)

// NewHTTPClient returns an *http.Client configured with the given timeout.
// Pass one of the Default*Timeout constants, or a custom duration.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}
