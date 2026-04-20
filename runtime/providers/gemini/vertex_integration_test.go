//go:build integration

// Vertex AI integration tests for the gemini provider.
//
// These tests exercise gemini against Google Vertex AI's publisher-models
// endpoint (rather than direct AI Studio) to prove the Vertex code path:
// URL shape (no `/models/` segment, no `?key=`) and Bearer-token auth via
// the GCP credential chain.
//
// Run locally:
//
//	gcloud auth application-default login
//	export GCP_PROJECT=<your-project>
//	export GCP_REGION=us-central1
//	export VERTEX_GEMINI_MODEL=gemini-2.0-flash-001   # optional
//	go test -tags=integration ./runtime/providers/gemini/... -run Vertex -v
package gemini

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

func vertexProject() string { return os.Getenv("GCP_PROJECT") }

func vertexRegion() string {
	if r := os.Getenv("GCP_REGION"); r != "" {
		return r
	}
	return "us-central1"
}

func vertexModel() string {
	if m := os.Getenv("VERTEX_GEMINI_MODEL"); m != "" {
		return m
	}
	return "gemini-2.0-flash-001"
}

// skipIfNoVertex skips when GCP project/credentials aren't available.
// Credential construction succeeds even with no logged-in identity, so the
// helper also issues a real token request to confirm the chain works.
func skipIfNoVertex(t *testing.T) {
	t.Helper()

	if vertexProject() == "" {
		t.Skip("GCP_PROJECT not set, skipping Vertex integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cred, err := credentials.NewGCPCredential(ctx, vertexProject(), vertexRegion())
	if err != nil {
		t.Skipf("GCP credentials not available (try: gcloud auth application-default login): %v", err)
	}

	probe, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", http.NoBody)
	if err := cred.Apply(ctx, probe); err != nil {
		t.Skipf("GCP token not available: %v", err)
	}
}

// vertexTestProvider builds a Vertex-configured gemini Provider, skipping
// the test if creds aren't available.
func vertexTestProvider(t *testing.T) *Provider {
	t.Helper()
	skipIfNoVertex(t)

	ctx := context.Background()
	cred, err := credentials.NewGCPCredential(ctx, vertexProject(), vertexRegion())
	if err != nil {
		t.Fatalf("failed to create GCP credential: %v", err)
	}

	platformConfig := &providers.PlatformConfig{
		Type:    "vertex",
		Region:  vertexRegion(),
		Project: vertexProject(),
	}

	// BaseURL is deliberately empty: this exercises the Arena path where the
	// gemini factory derives the publisher-models URL from PlatformConfig.
	return NewProviderWithCredential(
		"vertex-gemini-test", vertexModel(), "",
		providers.ProviderDefaults{
			MaxTokens:   256,
			Temperature: 0.1,
			Pricing: providers.Pricing{
				InputCostPer1K:  0.000075,
				OutputCostPer1K: 0.0003,
			},
		},
		false, cred, "vertex", platformConfig,
	)
}

func vertexTestToolProvider(t *testing.T) *ToolProvider {
	t.Helper()
	return &ToolProvider{Provider: vertexTestProvider(t)}
}

func TestVertex_BaseURLConstruction(t *testing.T) {
	skipIfNoVertex(t)

	p := vertexTestProvider(t)
	expected := vertexGeminiEndpoint(vertexRegion(), vertexProject())
	if p.baseURL != expected {
		t.Fatalf("baseURL = %q, want %q (Vertex publisher-models URL)", p.baseURL, expected)
	}
}

func TestVertex_GenerateContentURLShape(t *testing.T) {
	skipIfNoVertex(t)

	p := vertexTestProvider(t)
	url := p.generateContentURL("generateContent")

	if strings.Contains(url, "?key=") {
		t.Errorf("Vertex URL must not embed an API key query param: %s", url)
	}
	if !strings.Contains(url, "/publishers/google/models/"+vertexModel()+":generateContent") {
		t.Errorf("URL missing canonical Vertex path: %s", url)
	}
	if strings.Contains(url, "/models/models/") {
		t.Errorf("URL contains doubled /models/ segment: %s", url)
	}
}

func TestVertex_Predict(t *testing.T) {
	provider := vertexTestProvider(t)
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

func TestVertex_PredictWithTools(t *testing.T) {
	provider := vertexTestToolProvider(t)
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

func TestVertex_PredictStream(t *testing.T) {
	provider := vertexTestProvider(t)
	ctx := context.Background()

	stream, err := provider.PredictStream(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say hello in one word."},
		},
	})
	if err != nil {
		t.Fatalf("PredictStream failed: %v", err)
	}

	chunkCount := 0
	var lastChunk providers.StreamChunk
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

func TestVertex_ErrorOnInvalidModel(t *testing.T) {
	skipIfNoVertex(t)

	ctx := context.Background()
	cred, err := credentials.NewGCPCredential(ctx, vertexProject(), vertexRegion())
	if err != nil {
		t.Fatalf("failed to create GCP credential: %v", err)
	}

	provider := NewProviderWithCredential(
		"vertex-gemini-test-bad", "model-that-does-not-exist-xyz", "",
		providers.ProviderDefaults{MaxTokens: 100},
		false, cred, "vertex",
		&providers.PlatformConfig{
			Type: "vertex", Region: vertexRegion(), Project: vertexProject(),
		},
	)

	_, err = provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid model, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestVertex_CostCalculation(t *testing.T) {
	provider := vertexTestProvider(t)
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
