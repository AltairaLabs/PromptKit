package providers

import (
	"net/http"
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
