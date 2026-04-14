package tts

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

// ProviderSpec is the runtime form of a TTS-provider declaration,
// used by CreateFromSpec to construct a Service implementation.
// The SDK's runtime-config layer translates pkg/config.TTSProviderConfig
// into this struct after resolving credentials.
type ProviderSpec struct {
	// ID is a stable identifier; informational only at this layer.
	ID string
	// Type selects the implementation: openai, elevenlabs, cartesia.
	Type string
	// Model overrides the provider's default voice/model. Empty uses
	// the per-provider default.
	Model string
	// BaseURL overrides the provider's default API endpoint.
	BaseURL string
	// Credential carries the resolved API key.
	Credential credentials.Credential
	// AdditionalConfig carries provider-specific extras (cartesia
	// websocket URL, etc.). Unknown keys are ignored.
	AdditionalConfig map[string]any
}

// Factory builds a Service from a spec. Per-provider packages
// register one of these via init() so this package never needs to
// import them (avoiding a cycle — implementations already import
// this package for the Service interface).
type Factory func(spec ProviderSpec) (Service, error)

var (
	ttsFactoriesMu sync.RWMutex
	ttsFactories   = make(map[string]Factory)
)

// RegisterFactory registers a factory for the given provider type.
// Typically called from per-provider package init().
func RegisterFactory(providerType string, factory Factory) {
	ttsFactoriesMu.Lock()
	defer ttsFactoriesMu.Unlock()
	ttsFactories[providerType] = factory
}

// CreateFromSpec returns a Service implementation for the given spec.
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
func CreateFromSpec(spec ProviderSpec) (Service, error) {
	ttsFactoriesMu.RLock()
	factory, ok := ttsFactories[spec.Type]
	ttsFactoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unsupported TTS provider type: %s", spec.Type)
	}
	return factory(spec)
}

// ResolveCredential resolves a TTS provider's credential block
// into a concrete Credential, applying the same fallback chain as
// chat providers. Exposed as a helper for the SDK runtime-config
// layer.
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
// credential, or "" for any other credential shape (or nil). TTS
// providers want the key string for their constructors.
func APIKeyFromCredential(c credentials.Credential) string {
	if c == nil {
		return ""
	}
	if k, ok := c.(*credentials.APIKeyCredential); ok {
		return k.APIKey()
	}
	return ""
}
