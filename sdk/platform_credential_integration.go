package sdk

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// resolveBedrockCredential creates an AWS credential, using role assumption or
// named profile when configured, otherwise falling back to the default chain.
func resolveBedrockCredential(ctx context.Context, pc *platformConfig) (providers.Credential, error) {
	var (
		cred *credentials.AWSCredential
		err  error
	)
	switch {
	case pc.roleARN != "":
		cred, err = credentials.NewAWSCredentialWithRole(ctx, pc.region, pc.roleARN)
	case pc.awsProfile != "":
		cred, err = credentials.NewAWSCredentialWithProfile(ctx, pc.region, pc.awsProfile)
	default:
		cred, err = credentials.NewAWSCredential(ctx, pc.region)
	}
	if err != nil {
		return nil, fmt.Errorf("bedrock credentials: %w", err)
	}
	return cred, nil
}

// resolveVertexCredential creates a GCP credential, using a service account key
// file when configured, otherwise falling back to Application Default Credentials.
func resolveVertexCredential(ctx context.Context, pc *platformConfig) (providers.Credential, error) {
	var (
		cred *credentials.GCPCredential
		err  error
	)
	if pc.serviceAccountKeyPath != "" {
		cred, err = credentials.NewGCPCredentialWithServiceAccount(ctx, pc.project, pc.region, pc.serviceAccountKeyPath)
	} else {
		cred, err = credentials.NewGCPCredential(ctx, pc.project, pc.region)
	}
	if err != nil {
		return nil, fmt.Errorf("vertex credentials: %w", err)
	}
	return cred, nil
}

// resolveAzureCredential creates an Azure credential, using managed identity or
// client secret when configured, otherwise falling back to the default chain.
func resolveAzureCredential(ctx context.Context, pc *platformConfig) (providers.Credential, error) {
	var (
		cred *credentials.AzureCredential
		err  error
	)
	switch {
	case pc.azureClientSecret != nil:
		cs := pc.azureClientSecret
		cred, err = credentials.NewAzureCredentialWithClientSecret(
			ctx, pc.endpoint, cs.tenantID, cs.clientID, cs.clientSecret,
		)
	case pc.managedIdentityClientID != "":
		clientID := pc.managedIdentityClientID
		cred, err = credentials.NewAzureCredentialWithManagedIdentity(ctx, pc.endpoint, &clientID)
	default:
		cred, err = credentials.NewAzureCredential(ctx, pc.endpoint)
	}
	if err != nil {
		return nil, fmt.Errorf("azure credentials: %w", err)
	}
	return cred, nil
}
