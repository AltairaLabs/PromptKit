package tts

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// ProviderSpec is the runtime form of a TTS-provider declaration. It is a
// type alias for base.CapabilitySpec so the field shape is shared with STT,
// embedding, and image factories without code duplication.
type ProviderSpec = base.CapabilitySpec

// Factory builds a Service from a spec. Per-provider packages register one of
// these via init() so this package never needs to import them.
type Factory = base.Factory[Service]

var ttsRegistry = base.NewFactoryRegistry[Service]()

// RegisterFactory registers a factory for the given provider type.
// Typically called from per-provider package init().
func RegisterFactory(providerType string, factory Factory) {
	ttsRegistry.Register(providerType, factory)
}

// RegisteredTypes returns the TTS provider types with a registered factory,
// sorted. Use it to check a configured type before CreateFromSpec rather than
// constructing and parsing the error.
func RegisteredTypes() []string { return ttsRegistry.Types() }

// CreateFromSpec returns a Service implementation for the given spec.
func CreateFromSpec(spec ProviderSpec) (Service, error) {
	return ttsRegistry.Create(spec)
}

// ResolveCredential is a thin wrapper around base.ResolveCredential, kept here
// for back-compat with callers that resolve TTS-specific credential configs.
func ResolveCredential(
	ctx context.Context,
	providerType string,
	cfgDir string,
	cred *credentials.CredentialConfig,
) (credentials.Credential, error) {
	return base.ResolveCredential(ctx, providerType, cfgDir, cred)
}

// RegisterPlatforms declares the hosting platforms a provider type's factory
// wires (see base.FactoryRegistry.RegisterPlatforms).
func RegisterPlatforms(providerType string, platforms ...string) {
	ttsRegistry.RegisterPlatforms(providerType, platforms...)
}

// ResolveCredentialForPlatform resolves the credential a spec is built with.
// For a platform providerType runs on, that is the platform's credential
// chain; otherwise the platform is not consulted, so a provider that cannot
// run there still authenticates to its own API (CreateFromSpec then warns
// that the platform is ignored).
func ResolveCredentialForPlatform(
	ctx context.Context,
	providerType string,
	cfgDir string,
	cred *credentials.CredentialConfig,
	platform *credentials.PlatformConfig,
) (credentials.Credential, error) {
	if platform == nil || !ttsRegistry.SupportsPlatform(providerType, platform.Type) {
		return base.ResolveCredential(ctx, providerType, cfgDir, cred)
	}
	return base.ResolveCredentialForPlatform(ctx, providerType, cfgDir, cred, platform)
}

// APIKeyFromCredential returns the raw API key from an APIKey credential.
func APIKeyFromCredential(c credentials.Credential) string {
	return base.APIKeyFromCredential(c)
}

// PricingFromSpec extracts an optional pricing override from spec.AdditionalConfig.
func PricingFromSpec(spec ProviderSpec) *base.PricingDescriptor {
	return base.PricingFromAdditionalConfig(spec.AdditionalConfig)
}
