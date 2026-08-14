package providers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

// Embedding platform identifiers and the provider type that speaks the
// OpenAI embeddings wire format (the only one Azure hosting supports).
const (
	platformAzure       = "azure"
	platformBedrock     = "bedrock"
	platformVertex      = "vertex"
	embeddingTypeOpenAI = "openai"
	// embeddingTypeBedrock speaks Bedrock's per-family InvokeModel bodies
	// (Titan/Cohere) — see runtime/providers/bedrock.
	embeddingTypeBedrock = "bedrock"
)

// EmbeddingTransport is the resolved HTTP wiring for a vendor embedding
// factory. Exactly one of the two modes is populated:
//   - static key: Client==nil, PlatformAuth==false, APIKey may be set
//   - platform:   Client!=nil, PlatformAuth==true, APIKey==""
//
// BaseURL is the platform-resolved endpoint, or the spec's BaseURL
// override, or "" (provider keeps its built-in default).
type EmbeddingTransport struct {
	Client       *http.Client
	BaseURL      string
	APIKey       string
	PlatformAuth bool
}

// ResolveEmbeddingTransport decides between the static-API-key path and a
// hyperscaler-platform path for an embedding provider. It is the single
// place embedding factories consult, mirroring the chat path's
// credential/platform handling.
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
func ResolveEmbeddingTransport(spec EmbeddingProviderSpec) (EmbeddingTransport, error) {
	if spec.Platform == "" {
		return EmbeddingTransport{
			BaseURL: spec.BaseURL,
			APIKey:  APIKeyFromCredential(spec.Credential),
		}, nil
	}

	client, baseURL, err := buildPlatformEmbeddingClient(
		spec.Credential, spec.Platform, spec.Type, spec.Model, spec.PlatformConfig)
	if err != nil {
		return EmbeddingTransport{}, err
	}
	return EmbeddingTransport{
		Client:       client,
		BaseURL:      baseURL,
		PlatformAuth: true,
	}, nil
}

// buildPlatformEmbeddingClient resolves the platform endpoint and an HTTP
// client whose transport applies the credential per request. Azure is
// implemented; Bedrock/Vertex are gated until a platform-native embedding
// provider type exists (their wire format differs from OpenAI's).
func buildPlatformEmbeddingClient(
	cred credentials.Credential, platform, providerType, model string, pc *PlatformConfig,
) (*http.Client, string, error) {
	switch strings.ToLower(platform) {
	case platformAzure:
		if providerType != embeddingTypeOpenAI {
			return nil, "", fmt.Errorf(
				"embedding platform %q requires an OpenAI-wire-format provider (type=openai), got %q",
				platform, providerType)
		}
		if pc == nil || pc.Endpoint == "" {
			return nil, "", fmt.Errorf("embedding platform %q requires platform.endpoint", platform)
		}
		rt := &platformEmbeddingRoundTripper{
			base:       http.DefaultTransport,
			cred:       cred,
			apiVersion: azureEmbeddingAPIVersion(pc),
		}
		return &http.Client{Transport: rt}, credentials.AzureOpenAIEndpoint(pc.Endpoint, model), nil
	case platformBedrock:
		return buildBedrockEmbeddingClient(cred, providerType, pc)
	case platformVertex:
		return nil, "", fmt.Errorf(
			"embedding platform %q is not yet supported: it requires a platform-native embedding "+
				"provider type (Vertex body shapes), which is not implemented", platform)
	default:
		return nil, "", fmt.Errorf("unsupported embedding platform: %q", platform)
	}
}

// buildBedrockEmbeddingClient wires SigV4 auth and resolves the Bedrock
// runtime host. Unlike Azure it returns the bare host, not a per-model URL:
// Bedrock carries the model in the request path, so the provider appends
// /model/{modelId}/invoke and a per-request model override still works.
func buildBedrockEmbeddingClient(
	cred credentials.Credential, providerType string, pc *PlatformConfig,
) (*http.Client, string, error) {
	if providerType != embeddingTypeBedrock {
		return nil, "", fmt.Errorf(
			"embedding platform %q requires the platform-native provider type %q (Titan/Cohere "+
				"body shapes), got %q", platformBedrock, embeddingTypeBedrock, providerType)
	}

	baseURL := ""
	if pc != nil {
		baseURL = pc.Endpoint
		if baseURL == "" && pc.Region != "" {
			baseURL = credentials.BedrockEndpoint(pc.Region)
		}
	}
	if baseURL == "" {
		return nil, "", fmt.Errorf(
			"embedding platform %q requires platform.region (or an explicit platform.endpoint)",
			platformBedrock)
	}

	return &http.Client{
		Transport: &platformEmbeddingRoundTripper{base: http.DefaultTransport, cred: cred},
	}, baseURL, nil
}

// azureEmbeddingAPIVersion mirrors the chat path: prefer an explicit
// platform.additional_config.api_version, else the package default.
func azureEmbeddingAPIVersion(pc *PlatformConfig) string {
	if pc != nil {
		if v, ok := pc.AdditionalConfig["api_version"].(string); ok && v != "" {
			return v
		}
	}
	return credentials.DefaultAzureAPIVersion
}

// platformEmbeddingRoundTripper applies a credential (and, for Azure, the
// api-version query param) to every embedding request before delegating.
type platformEmbeddingRoundTripper struct {
	base       http.RoundTripper
	cred       credentials.Credential
	apiVersion string // non-empty only for Azure
}

// RoundTrip applies the api-version query param (Azure) and the credential
// to a cloned request before delegating to the base transport.
func (rt *platformEmbeddingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone before mutating: the RoundTripper contract forbids modifying
	// the caller's request.
	r := req.Clone(req.Context())
	if rt.apiVersion != "" && r.URL.Query().Get("api-version") == "" {
		q := r.URL.Query()
		q.Set("api-version", rt.apiVersion)
		r.URL.RawQuery = q.Encode()
	}
	if rt.cred != nil {
		if err := rt.cred.Apply(r.Context(), r); err != nil {
			return nil, err
		}
	}
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
