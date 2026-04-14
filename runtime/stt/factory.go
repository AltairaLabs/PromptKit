package stt

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

// ProviderSpec is the runtime form of an STT-provider declaration,
// used by CreateFromSpec to construct a Service implementation.
// The SDK's runtime-config layer translates pkg/config.STTProviderConfig
// into this struct after resolving credentials.
type ProviderSpec struct {
	// ID is a stable identifier; informational only at this layer.
	ID string
	// Type selects the implementation: openai (only one today).
	Type string
	// Model overrides the provider's default transcription model.
	Model string
	// BaseURL overrides the provider's default API endpoint.
	BaseURL string
	// Credential carries the resolved API key.
	Credential credentials.Credential
	// AdditionalConfig carries provider-specific extras. Unknown keys
	// are ignored.
	AdditionalConfig map[string]any
}

// Factory builds a Service from a spec.
type Factory func(spec ProviderSpec) (Service, error)

var (
	sttFactoriesMu sync.RWMutex
	sttFactories   = make(map[string]Factory)
)

// RegisterFactory registers a factory for the given provider type.
// Typically called from per-provider package init().
func RegisterFactory(providerType string, factory Factory) {
	sttFactoriesMu.Lock()
	defer sttFactoriesMu.Unlock()
	sttFactories[providerType] = factory
}

// CreateFromSpec returns a Service implementation for the given spec.
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
func CreateFromSpec(spec ProviderSpec) (Service, error) {
	sttFactoriesMu.RLock()
	factory, ok := sttFactories[spec.Type]
	sttFactoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unsupported STT provider type: %s", spec.Type)
	}
	return factory(spec)
}

// ResolveCredential resolves an STT provider's credential block
// into a concrete Credential, applying the same fallback chain as
// chat providers.
func ResolveCredential(ctx context.Context, providerType string,
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
func APIKeyFromCredential(c credentials.Credential) string {
	if c == nil {
		return ""
	}
	if k, ok := c.(*credentials.APIKeyCredential); ok {
		return k.APIKey()
	}
	return ""
}
