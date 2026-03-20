// Package httputil provides shared HTTP client construction utilities
// for the PromptKit project. It centralizes timeout defaults and client
// creation so that every module uses consistent configuration.
package httputil

import (
	"crypto/tls"
	"net"
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

	// DefaultStreamingTimeout is the HTTP timeout for streaming provider
	// responses (e.g. SSE streams). Streaming connections stay open much
	// longer than regular request/response cycles.
	DefaultStreamingTimeout = 300 * time.Second

	// Connection pool and transport defaults.
	defaultDialTimeout         = 30 * time.Second
	defaultDialKeepAlive       = 30 * time.Second
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 10
	defaultMaxConnsPerHost     = 10
	defaultIdleConnTimeout     = 90 * time.Second
	defaultTLSHandshakeTimeout = 10 * time.Second
)

// NewHTTPClient returns an *http.Client configured with the given timeout,
// a TLS 1.2 minimum, and connection pool limits.
// Pass one of the Default*Timeout constants, or a custom duration.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: newDefaultTransport(),
	}
}

// newDefaultTransport creates an HTTP transport with TLS 1.2 minimum and
// connection pooling defaults.
func newDefaultTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: defaultDialKeepAlive,
		}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12}, //#nosec G402
		MaxIdleConns:        defaultMaxIdleConns,
		MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
		MaxConnsPerHost:     defaultMaxConnsPerHost,
		IdleConnTimeout:     defaultIdleConnTimeout,
		TLSHandshakeTimeout: defaultTLSHandshakeTimeout,
		ForceAttemptHTTP2:   true,
	}
}
