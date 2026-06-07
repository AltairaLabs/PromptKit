//go:build integration

// Azure OpenAI embedding integration tests.
//
// These exercise the openai embedding provider against a REAL Azure OpenAI
// deployment using Azure AD token auth (no API key) — the keyless path added
// for the SharePoint RAG demo. They share the gating helpers (azureEndpoint,
// skipIfNoAzure, azureAPIVersionOverride) defined in azure_integration_test.go
// and are skipped unless an Azure token can actually be acquired.
//
// Run locally against the demo resource:
//
//	az login --scope https://cognitiveservices.azure.com/.default
//	export AZURE_OPENAI_ENDPOINT=https://aoai-omnia-demo-33eebkdcvsi4a.cognitiveservices.azure.com
//	export AZURE_OPENAI_EMBEDDING_DEPLOYMENT=text-embedding-3-small
//	go test -tags=integration ./runtime/providers/openai/... -run AzureEmbedding -v
//
// Optional:
//
//	AZURE_OPENAI_API_VERSION  api-version query param (default: credentials.DefaultAzureAPIVersion)
package openai

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// azureEmbeddingDeployment returns the embedding deployment to address. Azure
// routes by deployment name, not model name. Defaults to the demo resource's
// text-embedding-3-small deployment.
func azureEmbeddingDeployment() string {
	if d := os.Getenv("AZURE_OPENAI_EMBEDDING_DEPLOYMENT"); d != "" {
		return d
	}
	return "text-embedding-3-small"
}

// azureEmbeddingSpec builds the production EmbeddingProviderSpec for a keyless
// Azure deployment: a real AzureCredential + platform=azure, exactly as the
// SDK/memory-api paths assemble it.
func azureEmbeddingSpec(t *testing.T) providers.EmbeddingProviderSpec {
	t.Helper()
	skipIfNoAzure(t)

	cred, err := credentials.NewAzureCredential(context.Background(), azureEndpoint())
	if err != nil {
		t.Fatalf("failed to create Azure credential: %v", err)
	}

	pc := &providers.PlatformConfig{Type: "azure", Endpoint: azureEndpoint()}
	if v := azureAPIVersionOverride(); v != "" {
		pc.AdditionalConfig = map[string]any{"api_version": v}
	}

	return providers.EmbeddingProviderSpec{
		ID:             "azure-openai-embedding-test",
		Type:           "openai",
		Model:          azureEmbeddingDeployment(),
		Platform:       "azure",
		PlatformConfig: pc,
		Credential:     cred,
	}
}

// TestAzureEmbedding_FactoryBuildsDeploymentURL confirms the factory resolves
// the Azure deployment base URL from PlatformConfig (no api.openai.com default).
func TestAzureEmbedding_FactoryBuildsDeploymentURL(t *testing.T) {
	skipIfNoAzure(t)

	tr, err := providers.ResolveEmbeddingTransport(azureEmbeddingSpec(t))
	if err != nil {
		t.Fatalf("ResolveEmbeddingTransport failed: %v", err)
	}
	want := credentials.AzureOpenAIEndpoint(azureEndpoint(), azureEmbeddingDeployment())
	if tr.BaseURL != want {
		t.Fatalf("BaseURL = %q, want %q", tr.BaseURL, want)
	}
	if tr.Client == nil || !tr.PlatformAuth {
		t.Fatalf("expected platform client + PlatformAuth, got client=%v platformAuth=%v", tr.Client != nil, tr.PlatformAuth)
	}
}

// TestAzureEmbedding_Embed is the real end-to-end proof: it sends an embedding
// request to a live Azure OpenAI deployment authenticated with an Azure AD
// token (no API key) and asserts real vectors come back.
func TestAzureEmbedding_Embed(t *testing.T) {
	emb, err := providers.CreateEmbeddingProviderFromSpec(azureEmbeddingSpec(t))
	if err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec failed: %v", err)
	}

	ctx := context.Background()
	resp, err := emb.Embed(ctx, providers.EmbeddingRequest{
		Texts: []string{"PromptKit keyless Azure embedding works."},
	})
	if err != nil {
		t.Fatalf("Embed failed against live Azure: %v", err)
	}

	if len(resp.Embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(resp.Embeddings))
	}
	vec := resp.Embeddings[0]
	if len(vec) == 0 {
		t.Fatal("expected a non-empty embedding vector")
	}
	if len(vec) != emb.EmbeddingDimensions() {
		t.Errorf("vector length = %d, want provider dimensions %d", len(vec), emb.EmbeddingDimensions())
	}

	// A real embedding is not all-zeros.
	var nonZero bool
	for _, f := range vec {
		if f != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatal("embedding vector is all zeros — likely not a real response")
	}

	t.Logf("Azure embedding OK: model=%s dims=%d", resp.Model, len(vec))
}

// TestAzureEmbedding_BatchEmbed confirms multi-text requests return aligned
// vectors over the live deployment.
func TestAzureEmbedding_BatchEmbed(t *testing.T) {
	emb, err := providers.CreateEmbeddingProviderFromSpec(azureEmbeddingSpec(t))
	if err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec failed: %v", err)
	}

	ctx := context.Background()
	texts := []string{"first document", "second document", "third document"}
	resp, err := emb.Embed(ctx, providers.EmbeddingRequest{Texts: texts})
	if err != nil {
		t.Fatalf("batch Embed failed against live Azure: %v", err)
	}

	if len(resp.Embeddings) != len(texts) {
		t.Fatalf("expected %d embeddings, got %d", len(texts), len(resp.Embeddings))
	}
	for i, vec := range resp.Embeddings {
		if len(vec) == 0 {
			t.Fatalf("embedding %d is empty", i)
		}
	}
}
