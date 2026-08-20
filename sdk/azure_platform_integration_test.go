//go:build integration

// Azure platform integration test for the WithAzure provider path.
//
// The unit tests either side of this seam each cover half of the guarantee —
// that platformBaseURL defers for Azure, and that the openai factory turns a
// deferred base URL into a deployment path. Neither proves the halves meet, and
// #1811 was exactly a break between them: both sides were individually
// defensible and every call still 404d. Only a real request settles it.
//
// Run locally:
//
//	az login --scope https://cognitiveservices.azure.com/.default
//	export AZURE_OPENAI_ENDPOINT=https://<resource>.openai.azure.com
//	export AZURE_OPENAI_DEPLOYMENT=<deployment-name>
//	go test -tags=integration ./sdk/... -run Azure -v
package sdk

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// azurePlatformConfig builds the platformConfig WithAzure produces, or skips
// when the environment does not name a live deployment.
func azurePlatformConfig(t *testing.T) *platformConfig {
	t.Helper()

	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	if endpoint == "" {
		t.Skip("AZURE_OPENAI_ENDPOINT not set")
	}
	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if deployment == "" {
		deployment = "gpt-4o-mini"
	}

	return &platformConfig{
		platformType: platformTypeAzure,
		providerType: "openai",
		model:        deployment,
		endpoint:     endpoint,
	}
}

// The regression test for #1811. Before the fix this reached
// {endpoint}/chat/completions and Azure answered 404 for every call; the
// provider still constructed cleanly, so nothing short of a real request
// caught it.
func TestAzurePlatformProviderReachesDeployment(t *testing.T) {
	pc := azurePlatformConfig(t)

	prov, err := resolvePlatformProvider(&config{platform: pc})
	if err != nil {
		t.Fatalf("resolvePlatformProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := prov.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Reply with the single word: ok"}},
	})
	if err != nil {
		// A 404 here is the original bug resurfacing, so name it rather than
		// letting it read as a generic provider failure.
		if strings.Contains(err.Error(), "404") {
			t.Fatalf("Predict returned 404 — the deployment path is not being built (#1811): %v", err)
		}
		t.Fatalf("Predict: %v", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Error("Predict returned empty content")
	}
	t.Logf("Azure responded: %q", strings.TrimSpace(resp.Content))
}

// WithAzure and WithPlatformEndpoint write the same field, so a caller who
// worked around #1811 by passing a full deployment URL must keep working rather
// than having a second deployment path stacked onto the first.
func TestAzurePlatformProviderAcceptsFullDeploymentURL(t *testing.T) {
	pc := azurePlatformConfig(t)
	pc.endpoint = strings.TrimRight(pc.endpoint, "/") + "/openai/deployments/" + pc.model

	prov, err := resolvePlatformProvider(&config{platform: pc})
	if err != nil {
		t.Fatalf("resolvePlatformProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := prov.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Reply with the single word: ok"}},
	})
	if err != nil {
		t.Fatalf("Predict with a full deployment URL: %v", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Error("Predict returned empty content")
	}
	t.Logf("Azure responded: %q", strings.TrimSpace(resp.Content))
}
