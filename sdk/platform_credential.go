package sdk

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Credential constructor seams. These package-level variables default to the
// real runtime/credentials constructors and exist so the selection logic in the
// resolvers below can be observed in tests without a live cloud connection —
// tests swap them for fakes and assert which branch (and which arguments) were
// chosen. They must not be reassigned outside tests.
var (
	newAWSCredential            = credentials.NewAWSCredential
	newAWSCredentialWithProfile = credentials.NewAWSCredentialWithProfile
	newAWSCredentialWithRole    = credentials.NewAWSCredentialWithRole

	newGCPCredential               = credentials.NewGCPCredential
	newGCPCredentialWithServiceAcc = credentials.NewGCPCredentialWithServiceAccount

	newAzureCredential                    = credentials.NewAzureCredential
	newAzureCredentialWithClientSecret    = credentials.NewAzureCredentialWithClientSecret
	newAzureCredentialWithManagedIdentity = credentials.NewAzureCredentialWithManagedIdentity
)

// resolveBedrockCredential creates an AWS credential, using role assumption or
// named profile when configured, otherwise falling back to the default chain.
// roleARN takes precedence over awsProfile.
func resolveBedrockCredential(ctx context.Context, pc *platformConfig) (providers.Credential, error) {
	var (
		cred *credentials.AWSCredential
		err  error
	)
	switch {
	case pc.roleARN != "":
		cred, err = newAWSCredentialWithRole(ctx, pc.region, pc.roleARN)
	case pc.awsProfile != "":
		cred, err = newAWSCredentialWithProfile(ctx, pc.region, pc.awsProfile)
	default:
		cred, err = newAWSCredential(ctx, pc.region)
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
		cred, err = newGCPCredentialWithServiceAcc(ctx, pc.project, pc.region, pc.serviceAccountKeyPath)
	} else {
		cred, err = newGCPCredential(ctx, pc.project, pc.region)
	}
	if err != nil {
		return nil, fmt.Errorf("vertex credentials: %w", err)
	}
	return cred, nil
}

// resolveAzureCredential creates an Azure credential, using client secret or
// managed identity when configured, otherwise falling back to the default chain.
// A configured client secret takes precedence over managed identity.
func resolveAzureCredential(ctx context.Context, pc *platformConfig) (providers.Credential, error) {
	var (
		cred *credentials.AzureCredential
		err  error
	)
	switch {
	case pc.azureClientSecret != nil:
		cs := pc.azureClientSecret
		cred, err = newAzureCredentialWithClientSecret(
			ctx, pc.endpoint, cs.tenantID, cs.clientID, cs.clientSecret,
		)
	case pc.managedIdentityClientID != "":
		clientID := pc.managedIdentityClientID
		cred, err = newAzureCredentialWithManagedIdentity(ctx, pc.endpoint, &clientID)
	default:
		cred, err = newAzureCredential(ctx, pc.endpoint)
	}
	if err != nil {
		return nil, fmt.Errorf("azure credentials: %w", err)
	}
	return cred, nil
}
