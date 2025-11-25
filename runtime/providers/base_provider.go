package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// BaseProvider provides common functionality shared across all provider implementations.
// It should be embedded in concrete provider structs to avoid code duplication.
type BaseProvider struct {
	id               string
	includeRawOutput bool
	client           *http.Client
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

	client := &http.Client{Timeout: 60 * time.Second}
	return NewBaseProvider(id, includeRawOutput, client), apiKey
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

// CheckHTTPError checks if HTTP response is an error and returns formatted error with body
func CheckHTTPError(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body) // NOSONAR: Read error results in empty body in error message
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// UnmarshalJSON unmarshals JSON with error recovery that sets latency and raw response
func UnmarshalJSON(respBody []byte, v interface{}, predictResp *PredictionResponse, start time.Time) error {
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
