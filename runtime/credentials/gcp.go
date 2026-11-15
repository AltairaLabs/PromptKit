package credentials

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// gcpTokenRefreshBuffer is the time before token expiration to trigger a refresh.
const gcpTokenRefreshBuffer = 5 * time.Minute

// Vertex model publishers. The publisher is part of the URL, so a model from
// one cannot be reached through the other's endpoint.
const (
	vertexPublisherAnthropic = "anthropic"
	vertexPublisherGoogle    = "google"
)

// VertexEndpoint returns the Vertex AI endpoint URL for Anthropic models in a
// project and region. For Google-published models (embeddings) use
// VertexEmbeddingEndpoint.
func VertexEndpoint(project, region string) string {
	return vertexPublisherEndpoint(project, region, vertexPublisherAnthropic)
}

// VertexEmbeddingEndpoint returns the Vertex AI models base URL for Google's
// text-embedding models. Callers append "/{model}:predict".
//
// Kept separate from VertexEndpoint rather than parameterised at the call site
// because the publisher is easy to get wrong and wrong late: an anthropic-shaped
// URL for an embedding model is structurally valid and only fails when the
// request is made (#1301).
func VertexEmbeddingEndpoint(project, region string) string {
	return vertexPublisherEndpoint(project, region, vertexPublisherGoogle)
}

// DefaultVertexOpenAIEndpointID is the shared endpoint serving Vertex's
// OpenAI-compatible Chat Completions API for Gemini and MaaS partner models.
// Self-deployed Model Garden models use their own numeric endpoint ID instead.
const DefaultVertexOpenAIEndpointID = "openapi"

// VertexOpenAIEndpoint returns the base URL for Vertex AI's OpenAI-compatible
// Chat Completions API. Callers append "/chat/completions" — the OpenAI
// provider does this itself, so the helper stops short of it.
//
// endpointID selects the surface: empty means the shared "openapi" endpoint
// (Gemini plus MaaS partner models, zero configuration), and a numeric ID
// targets a self-deployed Model Garden endpoint.
//
// Separate from VertexEndpoint because this path differs in both API version
// (v1beta1) and shape (endpoints/{id} rather than publishers/{p}/models) — the
// two are not interchangeable, and substituting one produces a URL that is
// structurally valid and fails only at request time.
//
// Note: a self-deployed endpoint only speaks chat-completions when it is backed
// by a TGI, vLLM or HexLLM serving container deployed after 2024-08-20. That is
// not visible on the endpoint resource, so an older one fails at request time.
func VertexOpenAIEndpoint(project, region, endpointID string) string {
	if endpointID == "" {
		endpointID = DefaultVertexOpenAIEndpointID
	}
	return fmt.Sprintf("%s/v1beta1/%s/endpoints/%s", VertexHost(region), vertexLocationPath(project, region), endpointID)
}

// VertexHost returns the Vertex AI API origin serving a location: the bare
// host for "global", the .rep. host for the "us" and "eu" multi-regions, and
// {region}-aiplatform for a region. "{location}-aiplatform" does not resolve
// for global, us or eu, so every Vertex URL is built from this.
func VertexHost(location string) string {
	location = strings.ToLower(strings.TrimSpace(location))
	switch location {
	case "global":
		return "https://" + vertexAPIHost
	case "us", "eu":
		return "https://aiplatform." + location + ".rep.googleapis.com"
	default:
		return "https://" + location + "-" + vertexAPIHost
	}
}

// vertexAPIHost is the Vertex AI API's global host name.
const vertexAPIHost = "aiplatform.googleapis.com"

// VertexLocationBase returns the v1 REST base for a project's location:
// {host}/v1/projects/{project}/locations/{location}.
func VertexLocationBase(project, location string) string {
	return VertexHost(location) + "/v1/" + vertexLocationPath(project, location)
}

// vertexLivePath is the Live API (BidiGenerateContent) route on a location's host.
const vertexLivePath = "/ws/google.cloud.aiplatform.v1.LlmBidiService/BidiGenerateContent"

// VertexLiveURL returns the Live API WebSocket URL for a location.
func VertexLiveURL(location string) string {
	return "wss://" + strings.TrimPrefix(VertexHost(location), "https://") + vertexLivePath
}

// VertexGoogleModelResource returns the resource name of a Google-published
// model: projects/{project}/locations/{location}/publishers/google/models/{model}.
func VertexGoogleModelResource(project, location, model string) string {
	return vertexLocationPath(project, location) + "/publishers/" + vertexPublisherGoogle + "/models/" + model
}

// vertexLocationPath is projects/{project}/locations/{location}.
func vertexLocationPath(project, location string) string {
	return "projects/" + project + "/locations/" + location
}

// vertexPublisherEndpoint builds the models base URL for one publisher. Single
// format string so the exported helpers cannot drift.
func vertexPublisherEndpoint(project, region, publisher string) string {
	return VertexLocationBase(project, region) + "/publishers/" + publisher + "/models"
}

// Apply adds the OAuth2 token to the request.
func (c *GCPCredential) Apply(ctx context.Context, req *http.Request) error {
	token, err := c.getToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GCP token: %w", err)
	}

	req.Header.Set(headerAuthorization, bearerPrefix+token.AccessToken)
	return nil
}

// HeaderNames returns the header Apply sets.
func (c *GCPCredential) HeaderNames() []string { return []string{headerAuthorization} }

// Type returns "gcp".
func (c *GCPCredential) Type() string {
	return "gcp"
}

// Project returns the configured GCP project ID.
func (c *GCPCredential) Project() string {
	return c.project
}

// Region returns the configured GCP region.
func (c *GCPCredential) Region() string {
	return c.region
}

// getToken retrieves the current OAuth2 token, refreshing if necessary.
func (c *GCPCredential) getToken(_ context.Context) (*oauth2.Token, error) {
	c.mu.RLock()
	if c.cachedToken != nil && c.cachedToken.Valid() {
		token := c.cachedToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	// Need to refresh token
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.cachedToken != nil && c.cachedToken.Valid() {
		return c.cachedToken, nil
	}

	token, err := c.tokenSource.Token()
	if err != nil {
		return nil, err
	}

	// Add some buffer before expiry for token refresh
	if token.Expiry.After(time.Now().Add(gcpTokenRefreshBuffer)) {
		c.cachedToken = token
	}

	return token, nil
}
