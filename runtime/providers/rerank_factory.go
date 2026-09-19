package providers

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
)

// RerankProviderSpec is the transport-agnostic description the factory turns
// into a RerankProvider, mirroring EmbeddingProviderSpec. The SDK translates a
// pkg/config provider block into this after resolving credentials.
type RerankProviderSpec struct {
	// ID is a stable identifier for this instance.
	ID string
	// Type selects the implementation: voyageai, cohere, mock.
	Type string
	// Model overrides the provider's default rerank model.
	Model string
	// BaseURL overrides the provider's default API endpoint.
	BaseURL string
	// Credential carries the resolved API key. May be nil for
	// providers that need no auth (e.g. the in-process mock).
	Credential credentials.Credential
	// AdditionalConfig carries provider-specific extras. Unknown keys are
	// ignored, so a config written for one vendor does not fail on another.
	AdditionalConfig map[string]any
	// Platform identifies a hosting platform ("azure", "bedrock",
	// "vertex"). Empty means direct API access via Credential.
	Platform string
	// PlatformConfig holds platform-specific settings. Only set when
	// Platform != "".
	PlatformConfig *PlatformConfig
}

// RerankProviderFactory builds a RerankProvider from a spec. Per-provider
// packages register one via init() so this package never imports them — the
// implementations already import it for the interface, and the reverse would
// be a cycle.
type RerankProviderFactory func(spec RerankProviderSpec) (RerankProvider, error)

var (
	rerankFactoriesMu sync.RWMutex
	rerankFactories   = make(map[string]RerankProviderFactory)
)

// RegisterRerankProviderFactory registers a factory for the given provider
// type. Typically called from a per-provider package init().
// Re-registration overwrites silently, matching the embedding and chat paths.
func RegisterRerankProviderFactory(providerType string, factory RerankProviderFactory) {
	rerankFactoriesMu.Lock()
	defer rerankFactoriesMu.Unlock()
	rerankFactories[providerType] = factory
}

// RegisteredRerankProviderTypes returns the rerank provider types with a
// registered factory, sorted. Use it to check a configured type before
// CreateRerankProviderFromSpec rather than constructing and parsing the error.
func RegisteredRerankProviderTypes() []string {
	rerankFactoriesMu.RLock()
	defer rerankFactoriesMu.RUnlock()
	types := make([]string, 0, len(rerankFactories))
	for t := range rerankFactories {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// CreateRerankProviderFromSpec builds a rerank provider for spec.Type.
//
// This is the seam worth testing a new backend through: a factory that was
// never registered — an import missing, an init() that did not run — produces
// exactly this error, and a constructor test would not catch it because it
// calls the constructor directly.
func CreateRerankProviderFromSpec(spec RerankProviderSpec) (RerankProvider, error) {
	rerankFactoriesMu.RLock()
	factory, ok := rerankFactories[spec.Type]
	rerankFactoriesMu.RUnlock()
	if !ok {
		registered := RegisteredRerankProviderTypes()
		if len(registered) == 0 {
			return nil, fmt.Errorf(
				"unsupported rerank provider type %q: no rerank providers are registered "+
					"(import a provider package for its init(), e.g. runtime/providers/voyageai)",
				spec.Type)
		}
		return nil, fmt.Errorf("unsupported rerank provider type %q (registered: %v)",
			spec.Type, registered)
	}
	return factory(spec)
}

// ResolveRerankCredential resolves a rerank provider's credential block into a
// concrete Credential, applying the same fallback chain as the embedding and
// chat paths (api_key → file → env → default env vars).
func ResolveRerankCredential(ctx context.Context, providerType string,
	cfgDir string, cred *credentials.CredentialConfig, platform *credentials.PlatformConfig,
) (credentials.Credential, error) {
	return credentials.Resolve(ctx, credentials.ResolverConfig{
		ProviderType:     providerType,
		CredentialConfig: cred,
		ConfigDir:        cfgDir,
		PlatformConfig:   platform,
	})
}

// RerankTransport is the resolved transport for a rerank provider, mirroring
// EmbeddingTransport.
type RerankTransport struct {
	BaseURL string
	APIKey  string
}

// ResolveRerankTransport turns a spec's credential into the base URL and API
// key a vendor constructor needs.
//
// Platform-hosted reranking (Azure/Bedrock/Vertex) is not wired: no hyperscaler
// exposes a first-party rerank endpoint the way they do embeddings, so rather
// than guess at an endpoint shape this rejects the combination outright. A
// declared-but-unroutable platform would otherwise fall through to the direct
// API path and fail later with a confusing auth error. See #1330 for the
// platform-auth base layer this would build on.
func ResolveRerankTransport(spec RerankProviderSpec) (RerankTransport, error) {
	if spec.Platform != "" {
		return RerankTransport{}, fmt.Errorf(
			"rerank provider %q: platform %q is not supported for the rerank role; "+
				"configure the vendor's direct API instead",
			spec.idOrType(), spec.Platform)
	}
	return RerankTransport{
		BaseURL: spec.BaseURL,
		APIKey:  APIKeyFromCredential(spec.Credential),
	}, nil
}

// idOrType returns the spec's ID, falling back to its Type, for error text.
func (s RerankProviderSpec) idOrType() string {
	if s.ID != "" {
		return s.ID
	}
	return s.Type
}
