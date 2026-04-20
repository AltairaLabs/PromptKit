//go:build integration

// Vertex AI integration tests for the claude provider (Anthropic partner
// endpoint). Exercises the publishers/anthropic/models URL shape, the
// vertex-2023-10-16 anthropic_version body field, and Bearer-token auth
// via the GCP credential chain.
//
// Run locally:
//
//	gcloud auth application-default login
//	gcloud auth application-default set-quota-project <your-project>
//	export GCP_PROJECT=<your-project>
//	export GCP_REGION=us-east5    # optional; us-east5 hosts Anthropic models
//	export VERTEX_CLAUDE_MODEL=claude-haiku-4-5@20251001  # optional
//	go test -tags=integration ./runtime/providers/claude/... -run Vertex -v
//
// Tests skip if GCP credentials or quota project aren't available, or if
// Anthropic models aren't enabled in your Vertex Model Garden (which
// requires an explicit terms acceptance in the GCP console).
package claude

import (
	"context"
	"net/http"
	"os"
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
	return "us-east5"
}

func vertexClaudeModel() string {
	if m := os.Getenv("VERTEX_CLAUDE_MODEL"); m != "" {
		return m
	}
	return "claude-haiku-4-5@20251001"
}

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

func vertexClaudeTestProvider(t *testing.T) *Provider {
	t.Helper()
	skipIfNoVertex(t)

	ctx := context.Background()
	cred, err := credentials.NewGCPCredential(ctx, vertexProject(), vertexRegion())
	if err != nil {
		t.Fatalf("failed to create GCP credential: %v", err)
	}

	pc := &providers.PlatformConfig{
		Type:    "vertex",
		Region:  vertexRegion(),
		Project: vertexProject(),
	}

	// BaseURL deliberately empty: exercises the Arena path where the claude
	// factory derives the publishers/anthropic/models URL from PlatformConfig.
	return NewProviderWithCredential(
		"vertex-claude-test", vertexClaudeModel(), "",
		providers.ProviderDefaults{
			MaxTokens:   256,
			Temperature: 0.1,
			Pricing: providers.Pricing{
				InputCostPer1K:  0.001,
				OutputCostPer1K: 0.005,
			},
		},
		false, cred, "vertex", pc,
	)
}

func vertexClaudeTestToolProvider(t *testing.T) *ToolProvider {
	t.Helper()
	return &ToolProvider{Provider: vertexClaudeTestProvider(t)}
}

func TestClaudeVertex_BaseURLConstruction(t *testing.T) {
	skipIfNoVertex(t)

	p := vertexClaudeTestProvider(t)
	want := vertexAnthropicEndpoint(vertexRegion(), vertexProject())
	if p.baseURL != want {
		t.Fatalf("baseURL = %q, want %q (Vertex publishers/anthropic/models URL)", p.baseURL, want)
	}
}

func TestClaudeVertex_Predict(t *testing.T) {
	provider := vertexClaudeTestProvider(t)
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

func TestClaudeVertex_PredictWithTools(t *testing.T) {
	provider := vertexClaudeTestToolProvider(t)
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

func TestClaudeVertex_PredictStream(t *testing.T) {
	provider := vertexClaudeTestProvider(t)
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

func TestClaudeVertex_ErrorOnInvalidModel(t *testing.T) {
	skipIfNoVertex(t)

	ctx := context.Background()
	cred, err := credentials.NewGCPCredential(ctx, vertexProject(), vertexRegion())
	if err != nil {
		t.Fatalf("failed to create GCP credential: %v", err)
	}

	provider := NewProviderWithCredential(
		"vertex-claude-test-bad", "claude-model-that-does-not-exist-xyz", "",
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

func TestClaudeVertex_CostCalculation(t *testing.T) {
	provider := vertexClaudeTestProvider(t)
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
