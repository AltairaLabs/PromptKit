package providers

import (
	"context"
	"net/http"
)

const (
	// DefaultGeminiBaseURL is the default base URL for Gemini API (includes version path)
	DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// Registry manages available providers
type Registry struct {
	providers map[string]Provider
}

// ProviderFactory is a function that creates a provider from a spec
type ProviderFactory func(spec ProviderSpec) (Provider, error)

var providerFactories = make(map[string]ProviderFactory)

// RegisterProviderFactory registers a factory function for a provider type
func RegisterProviderFactory(providerType string, factory ProviderFactory) {
	providerFactories[providerType] = factory
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry using its ID as the key.
func (r *Registry) Register(provider Provider) {
	r.providers[provider.ID()] = provider
}

// Get retrieves a provider by ID, returning the provider and a boolean indicating if it was found.
func (r *Registry) Get(id string) (Provider, bool) {
	provider, exists := r.providers[id]
	return provider, exists
}

// List returns all registered provider IDs
func (r *Registry) List() []string {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// Close closes all registered providers and cleans up their resources.
// Returns the first error encountered, if any.
func (r *Registry) Close() error {
	for _, provider := range r.providers {
		if err := provider.Close(); err != nil {
			return err
		}
	}
	return nil
}

// ProviderSpec holds the configuration needed to create a provider instance
type ProviderSpec struct {
	ID               string
	Type             string
	Model            string
	BaseURL          string
	Defaults         ProviderDefaults
	IncludeRawOutput bool
	AdditionalConfig map[string]interface{} // Flexible key-value pairs for provider-specific configuration

	// Credential holds the resolved credential for this provider.
	// If nil, providers fall back to environment variable lookup.
	Credential Credential

	// Platform identifies the hosting platform (e.g., "bedrock", "vertex", "azure").
	// Empty string means direct API access to the provider.
	Platform string

	// PlatformConfig holds platform-specific configuration.
	// Only set when Platform is non-empty.
	PlatformConfig *PlatformConfig
}

// Credential applies authentication to HTTP requests.
// This is the interface that providers use to authenticate requests.
type Credential interface {
	// Apply adds authentication to the HTTP request.
	Apply(ctx context.Context, req *http.Request) error

	// Type returns the credential type identifier.
	Type() string
}

// PlatformConfig holds platform-specific settings from config.
type PlatformConfig struct {
	Type             string
	Region           string
	Project          string
	Endpoint         string
	AdditionalConfig map[string]interface{}
}

// CreateProviderFromSpec creates a provider implementation from a spec.
// Returns an error if the provider type is unsupported.
func CreateProviderFromSpec(spec ProviderSpec) (Provider, error) {
	// Use default base URLs if not specified
	baseURL := spec.BaseURL
	if baseURL == "" {
		switch spec.Type {
		case "openai":
			baseURL = "https://api.openai.com/v1"
		case "gemini":
			baseURL = DefaultGeminiBaseURL
		case "claude":
			baseURL = "https://api.anthropic.com"
		case "imagen":
			baseURL = DefaultGeminiBaseURL
		case "ollama":
			baseURL = "http://localhost:11434"
		case "vllm":
			baseURL = "http://localhost:8000"
		case "mock":
			// No base URL needed for mock provider
		}
	}

	// Update spec with default baseURL
	spec.BaseURL = baseURL

	// Look up the factory for this provider type
	factory, exists := providerFactories[spec.Type]
	if !exists {
		return nil, &UnsupportedProviderError{ProviderType: spec.Type}
	}

	return factory(spec)
}

// UnsupportedProviderError is returned when a provider type is not recognized
type UnsupportedProviderError struct {
	ProviderType string
}

// Error returns the error message for this unsupported provider error.
func (e *UnsupportedProviderError) Error() string {
	return "unsupported provider type: " + e.ProviderType
}

// HasCredential returns true if the spec has a real (non-empty, non-"none") credential.
// Use this in factory functions to decide between credential-based and env-var-based constructors.
func (s *ProviderSpec) HasCredential() bool {
	return s.Credential != nil && s.Credential.Type() != "none"
}

// CredentialFactory builds a ProviderFactory that routes between a credential-based
// constructor and an env-var-based constructor. This eliminates the duplicated
// init() pattern across provider packages.
//
// Usage in provider init():
//
//	providers.RegisterProviderFactory("claude", providers.CredentialFactory(
//	    func(spec providers.ProviderSpec) (providers.Provider, error) {
//	        return NewToolProviderWithCredential(spec.ID, spec.Model, ..., spec.Credential, ...), nil
//	    },
//	    func(spec providers.ProviderSpec) (providers.Provider, error) {
//	        return NewToolProvider(spec.ID, spec.Model, ...), nil
//	    },
//	))
func CredentialFactory(withCred, withoutCred ProviderFactory) ProviderFactory {
	return func(spec ProviderSpec) (Provider, error) {
		if spec.HasCredential() {
			return withCred(spec)
		}
		return withoutCred(spec)
	}
}
