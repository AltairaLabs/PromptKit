package credentials

import (
	"context"
	"fmt"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

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
