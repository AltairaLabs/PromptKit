package credentials

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// tokenRefreshBuffer is the time before token expiration to trigger a refresh.
const tokenRefreshBuffer = 5 * time.Minute

// AzureCredential implements Azure AD token-based authentication for Azure AI services.
type AzureCredential struct {
	endpoint    string
	cred        azcore.TokenCredential
	mu          sync.RWMutex
	cachedToken *azcore.AccessToken
}

// NewAzureCredential creates a new Azure credential using the default credential chain.
// This supports Managed Identity, Azure CLI, environment variables, and more.
func NewAzureCredential(ctx context.Context, endpoint string) (*AzureCredential, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	return &AzureCredential{
		endpoint: endpoint,
		cred:     cred,
	}, nil
}

// NewAzureCredentialWithClientSecret creates an Azure credential using client secret.
func NewAzureCredentialWithClientSecret(
	ctx context.Context, endpoint, tenantID, clientID, clientSecret string,
) (*AzureCredential, error) {
	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	return &AzureCredential{
		endpoint: endpoint,
		cred:     cred,
	}, nil
}

// NewAzureCredentialWithManagedIdentity creates an Azure credential using Managed Identity.
func NewAzureCredentialWithManagedIdentity(
	ctx context.Context, endpoint string, clientID *string,
) (*AzureCredential, error) {
	opts := &azidentity.ManagedIdentityCredentialOptions{}
	if clientID != nil && *clientID != "" {
		opts.ID = azidentity.ClientID(*clientID)
	}

	cred, err := azidentity.NewManagedIdentityCredential(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure managed identity credential: %w", err)
	}

	return &AzureCredential{
		endpoint: endpoint,
		cred:     cred,
	}, nil
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
