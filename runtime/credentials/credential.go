// Package credentials provides credential management for LLM provider authentication.
// It supports multiple credential types including API keys, AWS SigV4, GCP OAuth, and Azure AD.
package credentials

import (
	"context"
	"net/http"
)

// Credential applies authentication to HTTP requests.
// Implementations handle different authentication schemes like API keys,
// AWS SigV4 signing, OAuth tokens, etc.
type Credential interface {
	// Apply adds authentication to the HTTP request.
	// It may modify headers, query parameters, or the request body.
	Apply(ctx context.Context, req *http.Request) error

	// Type returns the credential type identifier (e.g., "api_key", "aws", "gcp", "azure").
	Type() string
}

// APIKeyCredential implements header-based API key authentication.
// It supports flexible header names for different providers.
type APIKeyCredential struct {
	apiKey     string
	headerName string
	prefix     string // Optional prefix like "Bearer "
}

// APIKeyOption configures an APIKeyCredential.
type APIKeyOption func(*APIKeyCredential)

// WithHeaderName sets the header name for the API key.
func WithHeaderName(name string) APIKeyOption {
	return func(c *APIKeyCredential) {
		c.headerName = name
	}
}

// WithBearerPrefix adds "Bearer " prefix to the API key.
func WithBearerPrefix() APIKeyOption {
	return func(c *APIKeyCredential) {
		c.prefix = "Bearer "
	}
}

// WithPrefix sets a custom prefix for the API key.
func WithPrefix(prefix string) APIKeyOption {
	return func(c *APIKeyCredential) {
		c.prefix = prefix
	}
}

// NewAPIKeyCredential creates a new API key credential.
// By default, it uses "Authorization" header with "Bearer " prefix.
func NewAPIKeyCredential(apiKey string, opts ...APIKeyOption) *APIKeyCredential {
	c := &APIKeyCredential{
		apiKey:     apiKey,
		headerName: "Authorization",
		prefix:     "Bearer ",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Apply adds the API key to the request header.
func (c *APIKeyCredential) Apply(_ context.Context, req *http.Request) error {
	if c.apiKey != "" {
		req.Header.Set(c.headerName, c.prefix+c.apiKey)
	}
	return nil
}

// Type returns "api_key".
func (c *APIKeyCredential) Type() string {
	return "api_key"
}

// APIKey returns the raw API key value.
// This is useful for providers that need the key for non-HTTP operations.
func (c *APIKeyCredential) APIKey() string {
	return c.apiKey
}

// NoOpCredential is a credential that does nothing.
// Used for providers that don't require authentication or handle it internally.
type NoOpCredential struct{}

// Apply does nothing.
func (c *NoOpCredential) Apply(_ context.Context, _ *http.Request) error {
	return nil
}

// Type returns "none".
func (c *NoOpCredential) Type() string {
	return "none"
}
