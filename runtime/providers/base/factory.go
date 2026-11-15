package base

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
	// Credential carries the resolved credential. For a platform-hosted
	// provider it is the platform's credential (an Entra token, say).
	Credential credentials.Credential
	// Platform hosts the provider on a cloud platform (e.g. azure). Nil
	// means the provider's own API. A type that does not declare the
	// platform (RegisterPlatforms) is built without it, with a warning.
	Platform *credentials.PlatformConfig
	// AdditionalConfig carries provider-specific extras. Unknown keys
	// are ignored.
	AdditionalConfig map[string]any
	// Tuning carries the provider file's headers, request_timeout and
	// http_transport. Create applies it after construction and rejects it
	// for an implementation that is not HTTPTunable.
	Tuning HTTPTuning
}

// Factory builds a typed Provider from a CapabilitySpec.
type Factory[T any] func(spec CapabilitySpec) (T, error)

// FactoryRegistry is a typed registry keyed by the implementation discriminator
// (spec.Type). Each capability package owns one registry instance.
type FactoryRegistry[T any] struct {
	mu        sync.RWMutex
	factories map[string]Factory[T]
	platforms map[string][]string
}

// NewFactoryRegistry creates an empty registry.
func NewFactoryRegistry[T any]() *FactoryRegistry[T] {
	return &FactoryRegistry[T]{factories: make(map[string]Factory[T]), platforms: make(map[string][]string)}
}

// RegisterPlatforms declares the hosting platforms implType's factory wires:
// it receives spec.Platform for them and builds the platform endpoint and
// credential itself.
func (r *FactoryRegistry[T]) RegisterPlatforms(implType string, platforms ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platforms[implType] = append(r.platforms[implType], platforms...)
}

// SupportsPlatform reports whether implType declared platform.
func (r *FactoryRegistry[T]) SupportsPlatform(implType, platform string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.platforms[implType] {
		// Case-insensitive, as the credential resolver matches it.
		if strings.EqualFold(p, platform) {
			return true
		}
	}
	return false
}

// ResolveCredential resolves the credential a spec of implType is built with.
// For a platform implType declares, that is the platform's credential chain
// (an Entra token, say); otherwise the platform is not consulted, so a type
// that cannot run there still authenticates to its own API, and Create
// warns that the platform is ignored.
func (r *FactoryRegistry[T]) ResolveCredential(
	ctx context.Context,
	implType string,
	cfgDir string,
	cred *credentials.CredentialConfig,
	platform *credentials.PlatformConfig,
) (credentials.Credential, error) {
	cfg := credentials.ResolverConfig{ProviderType: implType, CredentialConfig: cred, ConfigDir: cfgDir}
	if platform != nil && r.SupportsPlatform(implType, platform.Type) {
		cfg.PlatformConfig = platform
	}
	return credentials.Resolve(ctx, cfg)
}

// Register adds a factory under the given implementation type.
func (r *FactoryRegistry[T]) Register(implType string, f Factory[T]) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[implType] = f
}

// Types returns the implementation types with a registered factory, sorted.
// Callers use it to discover what Create will accept without attempting a
// construction and string-matching the error — e.g. a deploy adapter
// validating provider bindings before serving traffic.
func (r *FactoryRegistry[T]) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// Create dispatches to the registered factory for spec.Type, then applies
// spec.Tuning to the result.
//
// spec is a value-semantics builder; callers assemble inline.
func (r *FactoryRegistry[T]) Create(spec CapabilitySpec) (T, error) {
	r.mu.RLock()
	f, ok := r.factories[spec.Type]
	r.mu.RUnlock()
	var zero T
	if !ok {
		return zero, fmt.Errorf("unsupported provider type: %s", spec.Type)
	}
	if spec.Platform != nil && !r.SupportsPlatform(spec.Type, spec.Platform.Type) {
		// Built as before, against the provider's own API, so a config that
		// works with its own key keeps working; the warning says the platform
		// had no effect.
		WarnPlatformIgnored(spec.ID, spec.Type, spec.Platform.Type)
		spec.Platform = nil
	}
	v, err := f(spec)
	if err != nil {
		return v, err
	}
	if err := ApplyHTTPTuning(v, spec.Type, spec.Tuning); err != nil {
		return zero, err
	}
	return v, nil
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

// WarnPlatformIgnored logs that a provider type runs on no platform, so the
// one its spec names has no effect. Every role shares the message.
func WarnPlatformIgnored(id, providerType, platform string) {
	logger.Warn("provider type does not run on this platform; the platform is ignored",
		"provider", id, "type", providerType, "platform", platform)
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
