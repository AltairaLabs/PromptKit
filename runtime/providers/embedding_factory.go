package providers

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

// EmbeddingProviderSpec is the runtime form of an embedding-provider
// declaration, used by CreateEmbeddingProviderFromSpec to construct an
// EmbeddingProvider implementation. The SDK's runtime-config layer
// translates pkg/config.EmbeddingProviderConfig into this struct after
// resolving credentials.
type EmbeddingProviderSpec struct {
	// ID is a stable identifier; informational only at this layer.
	ID string
	// Type selects the implementation: openai, gemini, voyageai, ollama.
	Type string
	// Model overrides the provider's default embedding model. Empty
	// uses the per-provider default.
	Model string
	// BaseURL overrides the provider's default API endpoint.
	BaseURL string
	// Credential carries the resolved API key. May be nil for
	// providers that don't require auth (e.g. ollama).
	Credential credentials.Credential
	// AdditionalConfig carries provider-specific extras (voyage
	// dimensions/input_type, etc.). Unknown keys are ignored.
	AdditionalConfig map[string]any
}

// EmbeddingProviderFactory builds an EmbeddingProvider from a spec.
// Per-provider packages register one of these via init() so the
// providers package never needs to import them (avoiding a cycle —
// the implementations already import providers for the interface).
type EmbeddingProviderFactory func(spec EmbeddingProviderSpec) (EmbeddingProvider, error)

var (
	embeddingFactoriesMu sync.RWMutex
	embeddingFactories   = make(map[string]EmbeddingProviderFactory)
)

// RegisterEmbeddingProviderFactory registers a factory for the given
// provider type. Typically called from per-provider package init().
// Re-registration overwrites silently — matching RegisterProviderFactory
// for chat providers.
func RegisterEmbeddingProviderFactory(providerType string, factory EmbeddingProviderFactory) {
	embeddingFactoriesMu.Lock()
	defer embeddingFactoriesMu.Unlock()
	embeddingFactories[providerType] = factory
}

// CreateEmbeddingProviderFromSpec returns an EmbeddingProvider
// implementation for the given spec. Mirrors CreateProviderFromSpec
// for chat providers but is intentionally slimmer: embedding providers
// don't stream and don't need rate-limit or transport tuning today
// (call patterns are batch + short-lived).
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
func CreateEmbeddingProviderFromSpec(spec EmbeddingProviderSpec) (EmbeddingProvider, error) {
	embeddingFactoriesMu.RLock()
	factory, ok := embeddingFactories[spec.Type]
	embeddingFactoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unsupported embedding provider type: %s", spec.Type)
	}
	return factory(spec)
}

// ResolveEmbeddingCredential resolves an embedding provider's
// credential block into a concrete Credential, applying the same
// fallback chain as chat providers (api_key → file → env → default
// env vars). Exposed as a helper for the SDK runtime-config layer.
func ResolveEmbeddingCredential(ctx context.Context, providerType string,
	cfgDir string, cred *credentials.CredentialConfig,
) (credentials.Credential, error) {
	return credentials.Resolve(ctx, credentials.ResolverConfig{
		ProviderType:     providerType,
		CredentialConfig: cred,
		ConfigDir:        cfgDir,
	})
}

// APIKeyFromCredential returns the raw API key from an APIKey
// credential, or "" for any other credential shape (or nil).
// Embedding providers only need the key string, not the full
// header-application machinery — exposed here so per-provider
// init() functions can build their factory closures.
func APIKeyFromCredential(c credentials.Credential) string {
	if c == nil {
		return ""
	}
	if k, ok := c.(*credentials.APIKeyCredential); ok {
		return k.APIKey()
	}
	return ""
}

// IntFromConfig returns cfg[key] coerced to int, supporting the
// common YAML number shapes (int, int64, float64). Returns ok=false
// when the key is missing or the value isn't numeric.
func IntFromConfig(cfg map[string]any, key string) (int, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
