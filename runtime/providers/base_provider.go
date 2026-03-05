package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/time/rate"

	"github.com/AltairaLabs/PromptKit/pkg/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// Connection pooling defaults for HTTP transports shared across providers.
const (
	DefaultMaxIdleConns        = 1000
	DefaultMaxIdleConnsPerHost = 100
	DefaultMaxConnsPerHost     = 100
	DefaultIdleConnTimeout     = 90 * time.Second
	DefaultTLSHandshakeTimeout = 10 * time.Second
	DefaultDialTimeout         = 30 * time.Second
	DefaultDialKeepAlive       = 30 * time.Second
)

// NewPooledTransport creates an *http.Transport configured with connection
// pooling settings suitable for high-throughput provider communication.
func NewPooledTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   DefaultDialTimeout,
			KeepAlive: DefaultDialKeepAlive,
		}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:        DefaultMaxIdleConns,
		MaxIdleConnsPerHost: DefaultMaxIdleConnsPerHost,
		MaxConnsPerHost:     DefaultMaxConnsPerHost,
		IdleConnTimeout:     DefaultIdleConnTimeout,
		TLSHandshakeTimeout: DefaultTLSHandshakeTimeout,
		ForceAttemptHTTP2:   true,
	}
}

// BaseProvider provides common functionality shared across all provider implementations.
// It should be embedded in concrete provider structs to avoid code duplication.
type BaseProvider struct {
	id               string
	includeRawOutput bool
	client           *http.Client
	rateLimiter      *rate.Limiter
}

// NewBaseProvider creates a new BaseProvider with common fields
func NewBaseProvider(id string, includeRawOutput bool, client *http.Client) BaseProvider {
	return BaseProvider{
		id:               id,
		includeRawOutput: includeRawOutput,
		client:           client,
	}
}

// NewBaseProviderWithAPIKey creates a BaseProvider and retrieves API key from environment
// It tries the primary key first, then falls back to the secondary key if primary is empty.
func NewBaseProviderWithAPIKey(id string, includeRawOutput bool, primaryKey, fallbackKey string) (provider BaseProvider, apiKey string) {
	apiKey = os.Getenv(primaryKey)
	if apiKey == "" {
		apiKey = os.Getenv(fallbackKey)
	}

	client := &http.Client{
		Timeout:   httputil.DefaultProviderTimeout,
		Transport: NewPooledTransport(),
	}
	return NewBaseProvider(id, includeRawOutput, client), apiKey
}

// ExtractAPIKey extracts an API key string from a Credential, if it is an APIKeyCredential.
// Returns an empty string if the credential is nil, not an api_key type, or does not
// implement the APIKey() method.
func ExtractAPIKey(cred Credential) string {
	if cred == nil || cred.Type() != "api_key" {
		return ""
	}
	if akc, ok := cred.(interface{ APIKey() string }); ok {
		return akc.APIKey()
	}
	return ""
}

// NewBaseProviderWithCredential creates a BaseProvider with an explicit credential.
// It creates an HTTP client with the given timeout, builds the BaseProvider, and
// extracts the API key from the credential (if it is an api_key credential).
// This eliminates the duplicated credential-setup boilerplate across providers.
func NewBaseProviderWithCredential(
	id string, includeRawOutput bool, timeout time.Duration, cred Credential,
) (base BaseProvider, apiKey string) {
	client := &http.Client{
		Timeout:   timeout,
		Transport: NewPooledTransport(),
	}
	base = NewBaseProvider(id, includeRawOutput, client)
	apiKey = ExtractAPIKey(cred)
	return base, apiKey
}

// ID returns the provider ID
func (b *BaseProvider) ID() string {
	return b.id
}

// ShouldIncludeRawOutput returns whether to include raw API responses in output
func (b *BaseProvider) ShouldIncludeRawOutput() bool {
	return b.includeRawOutput
}

// Close closes the HTTP client's idle connections
func (b *BaseProvider) Close() error {
	if b.client != nil {
		b.client.CloseIdleConnections()
	}
	return nil
}

// SupportsStreaming returns true by default (can be overridden by providers that don't support streaming)
func (b *BaseProvider) SupportsStreaming() bool {
	return true
}

// GetHTTPClient returns the underlying HTTP client for provider-specific use
func (b *BaseProvider) GetHTTPClient() *http.Client {
	return b.client
}

// SetRateLimit configures per-provider rate limiting. requestsPerSecond controls
// the sustained rate, and burst controls how many requests can be made
// simultaneously before throttling kicks in. A zero or negative
// requestsPerSecond disables rate limiting (the default).
func (b *BaseProvider) SetRateLimit(requestsPerSecond float64, burst int) {
	if requestsPerSecond <= 0 {
		b.rateLimiter = nil
		return
	}
	b.rateLimiter = rate.NewLimiter(rate.Limit(requestsPerSecond), burst)
}

// RateLimiter returns the current rate limiter, or nil if rate limiting is
// not configured. This is useful for inspecting or sharing limiters.
func (b *BaseProvider) RateLimiter() *rate.Limiter {
	return b.rateLimiter
}

// WaitForRateLimit blocks until the rate limiter allows the request to
// proceed, or until the context is canceled. If no rate limiter is
// configured, it returns immediately. Providers should call this before
// making HTTP requests to respect rate limits.
func (b *BaseProvider) WaitForRateLimit(ctx context.Context) error {
	if b.rateLimiter == nil {
		return nil
	}
	return b.rateLimiter.Wait(ctx)
}

// CheckHTTPError checks if HTTP response is an error and returns formatted error with body
func CheckHTTPError(resp *http.Response, url string) error {
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body) // NOSONAR: Read error results in empty body in error message
		return fmt.Errorf("API request to %s failed with status %d: %s", url, resp.StatusCode, string(body))
	}
	return nil
}

// UnmarshalJSON unmarshals JSON with error recovery that sets latency and raw response
func UnmarshalJSON(respBody []byte, v any, predictResp *PredictionResponse, start time.Time) error {
	if err := json.Unmarshal(respBody, v); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return nil
}

// SetErrorResponse sets latency and raw body on error responses
func SetErrorResponse(predictResp *PredictionResponse, respBody []byte, start time.Time) {
	predictResp.Latency = time.Since(start)
	predictResp.Raw = respBody
}

// RequestHeaders is a map of HTTP header key-value pairs
type RequestHeaders map[string]string

// MakeJSONRequest performs a JSON HTTP POST request with common error handling.
// This reduces duplication across provider implementations.
// providerName is used for logging purposes.
func (b *BaseProvider) MakeJSONRequest(
	ctx context.Context,
	url string,
	request any,
	headers RequestHeaders,
	providerName string,
) ([]byte, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	return b.MakeRawRequest(ctx, url, reqBytes, headers, providerName)
}

// MakeRawRequest performs an HTTP POST request with pre-marshaled body.
// Use this when you need to control the serialization yourself.
func (b *BaseProvider) MakeRawRequest(
	ctx context.Context,
	url string,
	body []byte,
	headers RequestHeaders,
	providerName string,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set all headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Log the request (mask sensitive headers for logging)
	logHeaders := make(map[string]string)
	for k, v := range headers {
		if k == "Authorization" || k == "x-api-key" {
			logHeaders[k] = "***"
		} else {
			logHeaders[k] = v
		}
	}
	logger.APIRequest(providerName, "POST", url, logHeaders, json.RawMessage(body))

	// Wait for rate limiter before making the HTTP call
	if waitErr := b.WaitForRateLimit(ctx); waitErr != nil {
		return nil, fmt.Errorf("rate limit wait: %w", waitErr)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.APIResponse(providerName, resp.StatusCode, string(respBytes), nil)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}
