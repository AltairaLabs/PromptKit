package providers

import (
	"fmt"
	"net/http"
)

// PlatformEmbeddingSpec describes a platform-native embedding provider — one
// whose request bodies are the cloud's own rather than OpenAI-shaped, so it is
// selected by provider type and cannot be hosted on the OpenAI path.
type PlatformEmbeddingSpec struct {
	// PlatformHint completes the "requires a platform block (…)" error, naming
	// the fields that platform actually needs.
	PlatformHint string
	// Build constructs the provider once the transport has been resolved.
	Build func(EmbeddingProviderSpec, EmbeddingTransport) (EmbeddingProvider, error)
}

// RegisterPlatformEmbeddingProvider registers an embedding factory that enforces
// the platform block, resolves the transport, and delegates to Build.
//
// Platform-native providers have no API-key mode: without a platform block
// there is no signer or token source, so every request would go
// unauthenticated. Failing at construction beats failing once per request.
//
// Shared because the enforce-resolve-construct sequence is identical for every
// such provider; only the hint and the constructor differ.
func RegisterPlatformEmbeddingProvider(typeName string, spec PlatformEmbeddingSpec) {
	RegisterEmbeddingProviderFactory(typeName,
		func(s EmbeddingProviderSpec) (EmbeddingProvider, error) {
			if s.Platform == "" {
				return nil, fmt.Errorf(
					"%s embedding provider requires a platform block (%s)",
					typeName, spec.PlatformHint)
			}
			tr, err := ResolveEmbeddingTransport(s)
			if err != nil {
				return nil, err
			}
			return spec.Build(s, tr)
		},
	)
}

// EmbeddingWiring is the transport-derived configuration every platform-native
// embedding provider applies the same way. Family-specific settings — Cohere's
// input_type, Vertex's task_type — stay with their own provider.
type EmbeddingWiring struct {
	Model        string
	BaseURL      string
	Client       *http.Client
	PlatformAuth bool
	// Dimensions is 0 when the spec did not request one, which is what tells
	// the provider to keep its family default.
	Dimensions int
}

// EmbeddingWiringFrom extracts the shared wiring from a spec and its resolved
// transport.
func EmbeddingWiringFrom(spec EmbeddingProviderSpec, tr EmbeddingTransport) EmbeddingWiring {
	w := EmbeddingWiring{
		Model:        spec.Model,
		BaseURL:      tr.BaseURL,
		Client:       tr.Client,
		PlatformAuth: tr.PlatformAuth,
	}
	if dims, ok := IntFromConfig(spec.AdditionalConfig, "dimensions"); ok {
		w.Dimensions = dims
	}
	return w
}

// ApplyWiring applies the transport-derived settings, leaving each field alone
// when the wiring does not carry one. It reports whether Dimensions was set, so
// the caller knows not to overwrite it with a model-family default — a
// comparison against the default value cannot tell "unset" from "set to the
// same number".
func (b *BaseEmbeddingProvider) ApplyWiring(w EmbeddingWiring) (dimsExplicit bool) {
	if w.Model != "" {
		b.ProviderModel = w.Model
	}
	if w.BaseURL != "" {
		b.BaseURL = w.BaseURL
	}
	if w.Client != nil {
		b.HTTPClient = w.Client
	}
	if w.PlatformAuth {
		b.PlatformAuth = true
	}
	if w.Dimensions > 0 {
		b.Dimensions = w.Dimensions
		return true
	}
	return false
}
