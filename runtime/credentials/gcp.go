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

// VertexEndpoint returns the Vertex AI endpoint URL for a project and region.
func VertexEndpoint(project, region string) string {
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models",
		region, project, region)
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
