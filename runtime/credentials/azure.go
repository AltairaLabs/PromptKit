package credentials

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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

// azureFoundryProjectPath is the path segment under a Foundry account host
// that addresses a project: https://<account>.services.ai.azure.com/api/projects/<project>.
const azureFoundryProjectPath = "/api/projects/"

// azureOpenAIV1Path is the OpenAI v1 route under a Foundry project. It takes
// the deployment as the request's model and no api-version.
const azureOpenAIV1Path = "/openai/v1"

// Entra ID token scopes. An Azure OpenAI or Foundry account endpoint
// (<resource>.openai.azure.com, <resource>.cognitiveservices.azure.com,
// <account>.services.ai.azure.com) takes Cognitive Services tokens; a Foundry
// project endpoint answers 401 "audience is incorrect (https://ai.azure.com)"
// to them and takes only Foundry tokens.
const (
	azureCognitiveServicesScope = "https://cognitiveservices.azure.com/.default"
	azureFoundryScope           = "https://ai.azure.com/.default"
)

// AzureFoundryProject returns the Foundry project endpoint that endpoint
// addresses, without a trailing slash or anything after the project name, and
// whether it addresses one at all: a path of /api/projects/<project>, as a Foundry
// project endpoint has. The path decides, not the host, so a private endpoint
// or a sovereign-cloud host is recognized too.
func AzureFoundryProject(endpoint string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" {
		return "", false
	}
	i := strings.Index(u.Path, azureFoundryProjectPath)
	if i < 0 {
		return "", false
	}
	rest := u.Path[i+len(azureFoundryProjectPath):]
	project, _, _ := strings.Cut(rest, "/")
	if project == "" {
		return "", false
	}
	base := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path[:i] + azureFoundryProjectPath + project}
	return base.String(), true
}

// azureTokenScope is the Entra ID scope a token for endpoint must carry.
func azureTokenScope(endpoint string) string {
	if _, ok := AzureFoundryProject(endpoint); ok {
		return azureFoundryScope
	}
	return azureCognitiveServicesScope
}

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
//
// A Foundry project endpoint gets the project's OpenAI v1 route instead,
// which names the deployment in the request body, not the path:
//
//	https://<account>.services.ai.azure.com/api/projects/<project>
//	  -> https://<account>.services.ai.azure.com/api/projects/<project>/openai/v1
func AzureOpenAIEndpoint(endpoint, deployment string) string {
	if project, ok := AzureFoundryProject(endpoint); ok {
		return project + azureOpenAIV1Path
	}
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

	req.Header.Set(headerAuthorization, bearerPrefix+token.Token)
	return nil
}

// HeaderNames returns the header Apply sets.
func (c *AzureCredential) HeaderNames() []string { return []string{headerAuthorization} }

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

	// The scope follows the endpoint: Cognitive Services for an account,
	// Foundry for a project.
	token, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{azureTokenScope(c.endpoint)},
	})
	if err != nil {
		return nil, err
	}

	c.cachedToken = &token
	return &token, nil
}
