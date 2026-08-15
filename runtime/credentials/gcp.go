package credentials

import (
	"context"
	"fmt"
	"net/http"
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
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/endpoints/%s",
		region, project, region, endpointID)
}

// vertexPublisherEndpoint builds the models base URL for one publisher. Single
// format string so the two exported helpers cannot drift.
func vertexPublisherEndpoint(project, region, publisher string) string {
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/%s/models",
		region, project, region, publisher)
}

// Apply adds the OAuth2 token to the request.
func (c *GCPCredential) Apply(ctx context.Context, req *http.Request) error {
	token, err := c.getToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GCP token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	return nil
}

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
