//go:build integration

package credentials

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests validate the real cloud credential chain end-to-end.
// They require actual cloud credentials configured on the machine:
//   - AWS: ~/.aws/credentials or environment variables
//   - GCP: Application Default Credentials (gcloud auth application-default login)
//   - Azure: az login or environment variables
//
// Run with: go test -tags integration ./credentials/... -v -count=1

// ---------------------------------------------------------------------------
// AWS Bedrock
// ---------------------------------------------------------------------------

func awsRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r
	}
	return "eu-west-2"
}

func skipIfNoAWS(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := NewAWSCredential(ctx, awsRegion())
	if err != nil {
		t.Skipf("AWS credentials not available: %v", err)
	}
}

func TestIntegration_AWSCredential_DefaultChain(t *testing.T) {
	skipIfNoAWS(t)

	ctx := context.Background()
	cred, err := NewAWSCredential(ctx, awsRegion())
	require.NoError(t, err)

	assert.Equal(t, "aws", cred.Type())
	assert.Equal(t, awsRegion(), cred.Region())

	// Sign a real Bedrock-shaped request
	endpoint := BedrockEndpoint(awsRegion())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/model/anthropic.claude-3-haiku-20240307-v1:0/invoke",
		http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	err = cred.Apply(ctx, req)
	require.NoError(t, err)

	// Verify SigV4 headers are present
	assert.NotEmpty(t, req.Header.Get("Authorization"), "should have Authorization header")
	assert.Contains(t, req.Header.Get("Authorization"), "AWS4-HMAC-SHA256")
	assert.NotEmpty(t, req.Header.Get("X-Amz-Date"), "should have X-Amz-Date header")
	assert.NotEmpty(t, req.Header.Get("X-Amz-Content-Sha256"), "should have content hash")

	t.Logf("AWS credential: region=%s, auth header present, SigV4 signed", cred.Region())
}

// ---------------------------------------------------------------------------
// GCP Vertex AI
// ---------------------------------------------------------------------------

func gcpProject() string {
	if p := os.Getenv("GCP_PROJECT"); p != "" {
		return p
	}
	if p := os.Getenv("GOOGLE_CLOUD_PROJECT"); p != "" {
		return p
	}
	return "hsbc-vertex-001"
}

func gcpRegion() string {
	if r := os.Getenv("GCP_REGION"); r != "" {
		return r
	}
	return "europe-west2"
}

func skipIfNoGCP(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := NewGCPCredential(ctx, gcpProject(), gcpRegion())
	if err != nil {
		t.Skipf("GCP credentials not available: %v", err)
	}
}

func TestIntegration_GCPCredential_ADC(t *testing.T) {
	skipIfNoGCP(t)

	ctx := context.Background()
	cred, err := NewGCPCredential(ctx, gcpProject(), gcpRegion())
	require.NoError(t, err)

	assert.Equal(t, "gcp", cred.Type())
	assert.Equal(t, gcpProject(), cred.Project())
	assert.Equal(t, gcpRegion(), cred.Region())

	// Apply to a Vertex AI-shaped request
	endpoint := VertexEndpoint(gcpProject(), gcpRegion())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/claude-3-haiku-20240307:rawPredict",
		http.NoBody)
	require.NoError(t, err)

	err = cred.Apply(ctx, req)
	require.NoError(t, err)

	// Verify OAuth2 Bearer token is present
	auth := req.Header.Get("Authorization")
	assert.NotEmpty(t, auth, "should have Authorization header")
	assert.Contains(t, auth, "Bearer ", "should be a Bearer token")
	// Token should be substantial (not empty or short)
	assert.Greater(t, len(auth), 20, "token should be substantial")

	t.Logf("GCP credential: project=%s, region=%s, bearer token present (%d chars)",
		cred.Project(), cred.Region(), len(auth))
}

func TestIntegration_GCPCredential_TokenCaching(t *testing.T) {
	skipIfNoGCP(t)

	ctx := context.Background()
	cred, err := NewGCPCredential(ctx, gcpProject(), gcpRegion())
	require.NoError(t, err)

	// First request — fetches token
	req1, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", http.NoBody)
	require.NoError(t, cred.Apply(ctx, req1))
	token1 := req1.Header.Get("Authorization")

	// Second request — should use cached token (same value)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", http.NoBody)
	require.NoError(t, cred.Apply(ctx, req2))
	token2 := req2.Header.Get("Authorization")

	assert.Equal(t, token1, token2, "second request should use cached token")
}

// ---------------------------------------------------------------------------
// Azure
// ---------------------------------------------------------------------------

func azureEndpoint() string {
	if e := os.Getenv("AZURE_OPENAI_ENDPOINT"); e != "" {
		return e
	}
	return "https://promptkit-test.openai.azure.com"
}

func skipIfNoAzure(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cred, err := NewAzureCredential(ctx, azureEndpoint())
	if err != nil {
		t.Skipf("Azure credentials not available: %v", err)
	}
	// Actually try to get a token — credential creation succeeds even with
	// expired tokens, but Apply will fail.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", http.NoBody)
	if err := cred.Apply(ctx, req); err != nil {
		t.Skipf("Azure token not available (try: az login --scope https://cognitiveservices.azure.com/.default): %v", err)
	}
}

func TestIntegration_AzureCredential_DefaultChain(t *testing.T) {
	skipIfNoAzure(t)

	ctx := context.Background()
	cred, err := NewAzureCredential(ctx, azureEndpoint())
	require.NoError(t, err)

	assert.Equal(t, "azure", cred.Type())
	assert.Equal(t, azureEndpoint(), cred.Endpoint())

	// Apply to an Azure OpenAI-shaped request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		azureEndpoint()+"/openai/deployments/text-embedding-3-small/embeddings?api-version=2024-02-01",
		http.NoBody)
	require.NoError(t, err)

	err = cred.Apply(ctx, req)
	require.NoError(t, err)

	// Verify Bearer token is present
	auth := req.Header.Get("Authorization")
	assert.NotEmpty(t, auth, "should have Authorization header")
	assert.Contains(t, auth, "Bearer ", "should be a Bearer token")
	assert.Greater(t, len(auth), 20, "token should be substantial")

	t.Logf("Azure credential: endpoint=%s, bearer token present (%d chars)",
		cred.Endpoint(), len(auth))
}

func TestIntegration_AzureCredential_TokenCaching(t *testing.T) {
	skipIfNoAzure(t)

	ctx := context.Background()
	cred, err := NewAzureCredential(ctx, azureEndpoint())
	require.NoError(t, err)

	// First request
	req1, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", http.NoBody)
	require.NoError(t, cred.Apply(ctx, req1))
	token1 := req1.Header.Get("Authorization")

	// Second request — cached
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", http.NoBody)
	require.NoError(t, cred.Apply(ctx, req2))
	token2 := req2.Header.Get("Authorization")

	assert.Equal(t, token1, token2, "second request should use cached token")
}

// ---------------------------------------------------------------------------
// Platform resolver end-to-end
// ---------------------------------------------------------------------------

func TestIntegration_ResolvePlatformCredential_AWS(t *testing.T) {
	skipIfNoAWS(t)

	ctx := context.Background()
	cred, err := resolvePlatformCredential(ctx, ResolverConfig{
		PlatformConfig: &PlatformConfig{
			Type:   "bedrock",
			Region: awsRegion(),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "aws", cred.Type())
}

func TestIntegration_ResolvePlatformCredential_GCP(t *testing.T) {
	skipIfNoGCP(t)

	ctx := context.Background()
	cred, err := resolvePlatformCredential(ctx, ResolverConfig{
		PlatformConfig: &PlatformConfig{
			Type:    "vertex",
			Project: gcpProject(),
			Region:  gcpRegion(),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "gcp", cred.Type())
}

func TestIntegration_ResolvePlatformCredential_Azure(t *testing.T) {
	skipIfNoAzure(t)

	ctx := context.Background()
	cred, err := resolvePlatformCredential(ctx, ResolverConfig{
		PlatformConfig: &PlatformConfig{
			Type:     "azure",
			Endpoint: azureEndpoint(),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "azure", cred.Type())
}
