package stt

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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

// CreateFromSpec returns a Service implementation for the given spec.
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
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

// APIKeyFromCredential returns the raw API key from an APIKey credential.
func APIKeyFromCredential(c credentials.Credential) string {
	return base.APIKeyFromCredential(c)
}

// PricingFromSpec extracts an optional pricing override from spec.AdditionalConfig.
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
func PricingFromSpec(spec ProviderSpec) *base.PricingDescriptor {
	return base.PricingFromAdditionalConfig(spec.AdditionalConfig)
}
