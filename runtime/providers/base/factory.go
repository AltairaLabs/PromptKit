package base

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

// CapabilitySpec is the unified runtime form of a typed-provider declaration
// (TTS, STT, embedding, image gen). The SDK's runtime-config layer translates
// pkg/config provider configs into this struct after resolving credentials.
//
// Each capability package (tts, stt, embedding, image) aliases this type to
// preserve back-compat field-naming while sharing the factory machinery.
type CapabilitySpec struct {
	// ID is a stable identifier; informational only at this layer.
	ID string
	// Type selects the implementation (e.g. openai, elevenlabs, cartesia).
	Type string
	// Model overrides the provider's default model.
	Model string
	// BaseURL overrides the provider's default API endpoint.
	BaseURL string
	// Credential carries the resolved credential.
	Credential credentials.Credential
	// AdditionalConfig carries provider-specific extras. Unknown keys
	// are ignored.
	AdditionalConfig map[string]any
}

// Factory builds a typed Provider from a CapabilitySpec.
type Factory[T any] func(spec CapabilitySpec) (T, error)

// FactoryRegistry is a typed registry keyed by the implementation discriminator
// (spec.Type). Each capability package owns one registry instance.
type FactoryRegistry[T any] struct {
	mu        sync.RWMutex
	factories map[string]Factory[T]
}

// NewFactoryRegistry creates an empty registry.
func NewFactoryRegistry[T any]() *FactoryRegistry[T] {
	return &FactoryRegistry[T]{factories: make(map[string]Factory[T])}
}

// Register adds a factory under the given implementation type.
func (r *FactoryRegistry[T]) Register(implType string, f Factory[T]) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[implType] = f
}

// Create dispatches to the registered factory for spec.Type.
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
func (r *FactoryRegistry[T]) Create(spec CapabilitySpec) (T, error) {
	r.mu.RLock()
	f, ok := r.factories[spec.Type]
	r.mu.RUnlock()
	var zero T
	if !ok {
		return zero, fmt.Errorf("unsupported provider type: %s", spec.Type)
	}
	return f(spec)
}

// ResolveCredential resolves a provider's credential block into a concrete
// Credential. Shared by every typed-provider package's factory layer.
func ResolveCredential(
	ctx context.Context,
	providerType string,
	cfgDir string,
	cred *credentials.CredentialConfig,
) (credentials.Credential, error) {
	return credentials.Resolve(ctx, credentials.ResolverConfig{
		ProviderType:     providerType,
		CredentialConfig: cred,
		ConfigDir:        cfgDir,
	})
}

// APIKeyFromCredential returns the raw API key from an APIKey credential, or
// "" for any other credential shape (or nil).
func APIKeyFromCredential(c credentials.Credential) string {
	if c == nil {
		return ""
	}
	if k, ok := c.(*credentials.APIKeyCredential); ok {
		return k.APIKey()
	}
	return ""
}
