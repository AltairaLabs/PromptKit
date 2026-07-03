package credentials

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

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
