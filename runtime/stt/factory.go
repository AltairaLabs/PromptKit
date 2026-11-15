package stt

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// ProviderSpec is the runtime form of an STT-provider declaration. It is a
// type alias for base.CapabilitySpec so the field shape is shared with TTS,
// embedding, and image factories without code duplication.
type ProviderSpec = base.CapabilitySpec

// Factory builds a Service from a spec.
type Factory = base.Factory[Service]

var sttRegistry = base.NewFactoryRegistry[Service]()

// RegisterFactory registers a factory for the given provider type.
// Typically called from per-provider package init().
func RegisterFactory(providerType string, factory Factory) {
	sttRegistry.Register(providerType, factory)
}

// RegisteredTypes returns the STT provider types with a registered factory,
// sorted. Use it to check a configured type before CreateFromSpec rather than
// constructing and parsing the error.
func RegisteredTypes() []string { return sttRegistry.Types() }

// CreateFromSpec returns a Service implementation for the given spec.
func CreateFromSpec(spec ProviderSpec) (Service, error) {
	return sttRegistry.Create(spec)
}

// ResolveCredential is a thin wrapper around base.ResolveCredential, kept here
// for back-compat with callers that resolve STT-specific credential configs.
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
	sttRegistry.RegisterPlatforms(providerType, platforms...)
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
	if platform == nil || !sttRegistry.SupportsPlatform(providerType, platform.Type) {
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
