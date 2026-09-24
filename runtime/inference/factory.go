package inference

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// ProviderSpec is the runtime form of an inference-provider declaration.
// Aliased to base.CapabilitySpec so the field shape is shared with the TTS,
// STT, embedding, and image factories (id/type/model/base_url/credential/
// additional_config).
type ProviderSpec = base.CapabilitySpec

// Factory builds a Provider from a spec. Per-backend packages register one
// via init() so this package never imports them.
type Factory = base.Factory[Provider]

var inferenceRegistry = base.NewFactoryRegistry[Provider]()

// RegisterFactory registers a factory for the given provider type.
// Typically called from a per-backend package init().
func RegisterFactory(providerType string, f Factory) {
	inferenceRegistry.Register(providerType, f)
}

// RegisteredTypes returns the inference provider types with a registered
// factory, sorted. Use it to check a configured type before CreateFromSpec
// rather than constructing and parsing the error.
func RegisteredTypes() []string { return inferenceRegistry.Types() }

// CreateFromSpec builds a Provider for the spec's Type.
func CreateFromSpec(spec ProviderSpec) (Provider, error) {
	return inferenceRegistry.Create(spec)
}

// ResolveCredential is a thin wrapper around base.ResolveCredential,
// matching tts.ResolveCredential / stt.ResolveCredential / classify.ResolveCredential.
func ResolveCredential(
	ctx context.Context,
	providerType string,
	cfgDir string,
	cred *credentials.CredentialConfig,
) (credentials.Credential, error) {
	return base.ResolveCredential(ctx, providerType, cfgDir, cred)
}
