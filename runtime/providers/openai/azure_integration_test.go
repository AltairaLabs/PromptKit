//go:build integration

// Package openai integration tests for Azure OpenAI / Azure AI Foundry.
//
// These tests exercise the openai provider against a real Azure deployment.
// They are gated by the `integration` build tag and skipped when Azure
// credentials or the AZURE_OPENAI_ENDPOINT env var are unavailable.
//
// Run locally:
//
//	az login --scope https://cognitiveservices.azure.com/.default
//	export AZURE_OPENAI_ENDPOINT=https://<resource>.openai.azure.com
//	export AZURE_OPENAI_DEPLOYMENT=<deployment-name>   # e.g. gpt-4o-mini
//	go test -tags=integration ./runtime/providers/openai/... -run Azure -v
//
// Optional:
//
//	AZURE_OPENAI_API_VERSION  api-version query param (default: 2024-12-01-preview)
package openai

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// azureEndpoint returns the configured Azure OpenAI / Foundry resource endpoint.
func azureEndpoint() string {
	return os.Getenv("AZURE_OPENAI_ENDPOINT")
}

// azureDeployment returns the deployment name to address. Azure routes by
// deployment, not model name.
func azureDeployment() string {
	if d := os.Getenv("AZURE_OPENAI_DEPLOYMENT"); d != "" {
		return d
	}
	return "gpt-4o-mini"
}

// azureAPIVersionOverride lets tests pin a specific api-version. Empty falls
// back to credentials.DefaultAzureAPIVersion.
func azureAPIVersionOverride() string {
	return os.Getenv("AZURE_OPENAI_API_VERSION")
}

// skipIfNoAzure skips the test unless Azure credentials are available AND a
// token can actually be acquired for the cognitive services scope. Credential
// construction succeeds even with no logged-in identity, so we must Apply()
// to confirm the chain works end-to-end.
func skipIfNoAzure(t *testing.T) {
	t.Helper()

	if azureEndpoint() == "" {
		t.Skip("AZURE_OPENAI_ENDPOINT not set, skipping Azure integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cred, err := credentials.NewAzureCredential(ctx, azureEndpoint())
	if err != nil {
		t.Skipf("Azure credentials not available: %v", err)
	}

	probe, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", http.NoBody)
	if err := cred.Apply(ctx, probe); err != nil {
		t.Skipf("Azure token not available (try: az login --scope https://cognitiveservices.azure.com/.default): %v", err)
	}
}

// azureTestProvider builds an openai Provider configured for Azure and skips
// the test if credentials are unavailable.
func azureTestProvider(t *testing.T) *Provider {
	t.Helper()
	skipIfNoAzure(t)

	ctx := context.Background()
	cred, err := credentials.NewAzureCredential(ctx, azureEndpoint())
	if err != nil {
		t.Fatalf("failed to create Azure credential: %v", err)
	}

	platformConfig := &providers.PlatformConfig{
		Type:     "azure",
		Endpoint: azureEndpoint(),
	}
	if v := azureAPIVersionOverride(); v != "" {
		platformConfig.AdditionalConfig = map[string]any{"api_version": v}
	}

	return NewProviderFromConfig(&ProviderConfig{
		ID:    "azure-openai-test",
		Model: azureDeployment(),
		// BaseURL deliberately empty — exercises issue #1010 fix where the
		// Azure URL is built by the factory from PlatformConfig rather than
		// being clobbered by the registry's api.openai.com default.
		BaseURL: "",
		Defaults: providers.ProviderDefaults{
			MaxTokens:   256,
			Temperature: 0.1,
			Pricing: providers.Pricing{
				InputCostPer1K:  0.00015,
				OutputCostPer1K: 0.0006,
			},
		},
		Credential:     cred,
		Platform:       "azure",
		PlatformConfig: platformConfig,
	})
}

func azureTestToolProvider(t *testing.T) *ToolProvider {
	t.Helper()
	return &ToolProvider{Provider: azureTestProvider(t)}
}

func TestAzure_BaseURLConstruction(t *testing.T) {
	skipIfNoAzure(t)

	p := azureTestProvider(t)
	expected := credentials.AzureOpenAIEndpoint(azureEndpoint(), azureDeployment())
	if p.baseURL != expected {
		t.Fatalf("baseURL = %q, want %q (Azure deployment URL)", p.baseURL, expected)
	}
}

func TestAzure_ChatCompletionsURLAppendsAPIVersion(t *testing.T) {
	skipIfNoAzure(t)

	p := azureTestProvider(t)
	url := p.chatCompletionsURL()
	if !strings.Contains(url, "/openai/deployments/"+azureDeployment()+"/chat/completions") {
		t.Errorf("URL missing deployment path: %s", url)
	}
	if !strings.Contains(url, "api-version=") {
		t.Errorf("URL missing api-version query param: %s", url)
	}
}

func TestAzure_Predict(t *testing.T) {
	provider := azureTestProvider(t)
	ctx := context.Background()

	resp, err := provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say hello in one word."},
		},
	})
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}

	if resp.Content == "" {
		t.Fatal("expected non-empty response content")
	}
	t.Logf("Response: %s", resp.Content)

	if resp.CostInfo == nil {
		t.Fatal("expected CostInfo to be set")
	}
}

func TestAzure_PredictWithTools(t *testing.T) {
	provider := azureTestToolProvider(t)
	ctx := context.Background()

	tools, err := provider.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather for a city",
			InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		},
	})
	if err != nil {
		t.Fatalf("BuildTooling failed: %v", err)
	}

	resp, toolCalls, err := provider.PredictWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What is the weather in Paris?"},
		},
	}, tools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	if resp.Content == "" && len(toolCalls) == 0 {
		t.Fatal("expected either text content or tool calls")
	}

	if len(toolCalls) > 0 {
		t.Logf("Tool call: %s(%s)", toolCalls[0].Name, string(toolCalls[0].Args))
	} else {
		t.Logf("Text response: %s", resp.Content)
	}
}

func TestAzure_PredictStream(t *testing.T) {
	provider := azureTestProvider(t)
	ctx := context.Background()

	stream, err := provider.PredictStream(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say hello in one word."},
		},
	})
	if err != nil {
		t.Fatalf("PredictStream failed: %v", err)
	}

	var lastChunk providers.StreamChunk
	chunkCount := 0
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		lastChunk = chunk
		chunkCount++
	}

	if chunkCount == 0 {
		t.Fatal("expected at least one stream chunk")
	}
	if lastChunk.Content == "" {
		t.Fatal("expected non-empty content in final chunk")
	}
	t.Logf("Stream response (%d chunks): %s", chunkCount, lastChunk.Content)
}

func TestAzure_ErrorOnInvalidDeployment(t *testing.T) {
	skipIfNoAzure(t)

	ctx := context.Background()
	cred, err := credentials.NewAzureCredential(ctx, azureEndpoint())
	if err != nil {
		t.Fatalf("failed to create Azure credential: %v", err)
	}

	provider := NewProviderFromConfig(&ProviderConfig{
		ID:      "azure-openai-test-bad",
		Model:   "deployment-that-does-not-exist-xyz",
		BaseURL: "",
		Defaults: providers.ProviderDefaults{
			MaxTokens: 100,
		},
		Credential: cred,
		Platform:   "azure",
		PlatformConfig: &providers.PlatformConfig{
			Type:     "azure",
			Endpoint: azureEndpoint(),
		},
	})

	_, err = provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid deployment, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestAzure_CostCalculation(t *testing.T) {
	provider := azureTestProvider(t)
	ctx := context.Background()

	resp, err := provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say hello in one word."},
		},
	})
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}

	if resp.CostInfo == nil {
		t.Fatal("expected CostInfo to be set")
	}
	if resp.CostInfo.InputTokens <= 0 {
		t.Errorf("expected positive InputTokens, got %d", resp.CostInfo.InputTokens)
	}
	if resp.CostInfo.OutputTokens <= 0 {
		t.Errorf("expected positive OutputTokens, got %d", resp.CostInfo.OutputTokens)
	}
	if resp.CostInfo.TotalCost <= 0 {
		t.Errorf("expected positive TotalCost, got %f", resp.CostInfo.TotalCost)
	}

	t.Logf("Cost: input=%d tokens, output=%d tokens, total=$%.6f",
		resp.CostInfo.InputTokens, resp.CostInfo.OutputTokens, resp.CostInfo.TotalCost)
}
