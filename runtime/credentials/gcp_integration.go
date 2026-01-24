package credentials

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// gcpTokenRefreshBuffer is the time before token expiration to trigger a refresh.
const gcpTokenRefreshBuffer = 5 * time.Minute

// VertexEndpoint returns the Vertex AI endpoint URL for a project and region.
func VertexEndpoint(project, region string) string {
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models",
		region, project, region)
}

// GCPCredential implements OAuth2 token-based authentication for Vertex AI.
type GCPCredential struct {
	project     string
	region      string
	tokenSource oauth2.TokenSource
	mu          sync.RWMutex
	cachedToken *oauth2.Token
}

// NewGCPCredential creates a new GCP credential using Application Default Credentials.
// This supports Workload Identity, service account keys, and gcloud auth.
func NewGCPCredential(ctx context.Context, project, region string) (*GCPCredential, error) {
	// Use Application Default Credentials
	tokenSource, err := google.DefaultTokenSource(ctx,
		"https://www.googleapis.com/auth/cloud-platform",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create token source: %w", err)
	}

	return &GCPCredential{
		project:     project,
		region:      region,
		tokenSource: tokenSource,
	}, nil
}

// NewGCPCredentialWithServiceAccount creates a GCP credential from a service account key file.
func NewGCPCredentialWithServiceAccount(ctx context.Context, project, region, keyFile string) (*GCPCredential, error) {
	data, err := readCredentialFile(keyFile, "")
	if err != nil {
		return nil, fmt.Errorf("failed to read service account key: %w", err)
	}

	config, err := google.JWTConfigFromJSON([]byte(data),
		"https://www.googleapis.com/auth/cloud-platform",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse service account key: %w", err)
	}

	return &GCPCredential{
		project:     project,
		region:      region,
		tokenSource: config.TokenSource(ctx),
	}, nil
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
