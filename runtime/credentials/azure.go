package credentials

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// tokenRefreshBuffer is the time before token expiration to trigger a refresh.
const tokenRefreshBuffer = 5 * time.Minute

// DefaultAzureAPIVersion is the default Azure OpenAI API version used when
// platform.additional_config.api_version is not set.
const DefaultAzureAPIVersion = "2024-12-01-preview"

// azureDeploymentPath is the path segment under an Azure OpenAI account host
// that addresses a single deployment.
const azureDeploymentPath = "/openai/deployments/"

// AzureOpenAIEndpoint returns the base URL for an Azure OpenAI deployment.
// The returned URL does NOT include the API path (/chat/completions) or
// api-version query param — the provider appends those per-request.
//
// endpoint is normally an account host, which gets the deployment path
// appended:
//
//	https://<resource>.openai.azure.com
//	  -> https://<resource>.openai.azure.com/openai/deployments/<deployment>
//
// An endpoint that already addresses a deployment is returned as-is instead of
// having a second deployment path stacked onto it, so a caller that passes a
// full deployment URL still ends up with a usable base.
func AzureOpenAIEndpoint(endpoint, deployment string) string {
	trimmed := strings.TrimRight(endpoint, "/")
	if strings.Contains(trimmed, azureDeploymentPath) {
		return trimmed
	}
	return trimmed + azureDeploymentPath + deployment
}

// Apply adds the Azure AD token to the request.
func (c *AzureCredential) Apply(ctx context.Context, req *http.Request) error {
	token, err := c.getToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Azure token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)
	return nil
}

// Type returns "azure".
func (c *AzureCredential) Type() string {
	return "azure"
}

// Endpoint returns the configured Azure endpoint.
func (c *AzureCredential) Endpoint() string {
	return c.endpoint
}

// getToken retrieves the current Azure AD token, refreshing if necessary.
func (c *AzureCredential) getToken(ctx context.Context) (*azcore.AccessToken, error) {
	c.mu.RLock()
	if c.cachedToken != nil && c.cachedToken.ExpiresOn.After(time.Now().Add(tokenRefreshBuffer)) {
		token := c.cachedToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	// Need to refresh token
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.cachedToken != nil && c.cachedToken.ExpiresOn.After(time.Now().Add(tokenRefreshBuffer)) {
		return c.cachedToken, nil
	}

	// Request token for Azure Cognitive Services scope
	token, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://cognitiveservices.azure.com/.default"},
	})
	if err != nil {
		return nil, err
	}

	c.cachedToken = &token
	return &token, nil
}
