//go:build integration

package claude

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// hasAWSCredentials checks whether AWS credentials are available via env vars or shared credentials file.
func hasAWSCredentials() bool {
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		return true
	}
	// Check for shared credentials file (~/.aws/credentials)
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(home + "/.aws/credentials")
	return err == nil
}

// bedrockTestProvider creates a Provider configured for Bedrock integration tests.
// It skips the test if AWS credentials are not available.
//
// Configuration via environment variables:
//   - AWS_REGION: AWS region (default: us-east-1)
//   - BEDROCK_MODEL: override the Bedrock model ID (default: mapped from claude-3-5-haiku-20241022)
func bedrockTestProvider(t *testing.T) *Provider {
	t.Helper()

	if !hasAWSCredentials() {
		t.Skip("no AWS credentials available, skipping Bedrock integration test")
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-west-2"
	}

	ctx := context.Background()
	cred, err := credentials.NewAWSCredential(ctx, region)
	if err != nil {
		t.Fatalf("failed to create AWS credential: %v", err)
	}

	// Allow override via BEDROCK_MODEL for accounts that require inference profile IDs
	bedrockModel := os.Getenv("BEDROCK_MODEL")
	if bedrockModel == "" {
		model := "claude-3-5-haiku-20241022"
		var ok bool
		bedrockModel, ok = credentials.BedrockModelMapping[model]
		if !ok {
			t.Fatalf("model %q not found in BedrockModelMapping", model)
		}
	}

	baseURL := credentials.BedrockEndpoint(region)

	return NewProviderWithCredential(
		"bedrock-test", bedrockModel, baseURL,
		providers.ProviderDefaults{
			MaxTokens:   256,
			Temperature: 0.1,
			Pricing: providers.Pricing{
				InputCostPer1K:  0.001,
				OutputCostPer1K: 0.005,
			},
		},
		false, cred, "bedrock", nil,
	)
}

// bedrockTestToolProvider creates a ToolProvider configured for Bedrock integration tests.
func bedrockTestToolProvider(t *testing.T) *ToolProvider {
	t.Helper()
	return &ToolProvider{Provider: bedrockTestProvider(t)}
}

func TestBedrock_Predict(t *testing.T) {
	provider := bedrockTestProvider(t)
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

func TestBedrock_PredictWithTools(t *testing.T) {
	provider := bedrockTestToolProvider(t)
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

	// The model should either respond with text or make a tool call
	if resp.Content == "" && len(toolCalls) == 0 {
		t.Fatal("expected either text content or tool calls")
	}

	if len(toolCalls) > 0 {
		t.Logf("Tool call: %s(%s)", toolCalls[0].Name, string(toolCalls[0].Args))
	} else {
		t.Logf("Text response: %s", resp.Content)
	}
}

func TestBedrock_PredictMultimodal(t *testing.T) {
	provider := bedrockTestProvider(t)
	ctx := context.Background()

	// Text-only multimodal request — validates the multimodal code path without needing an image
	resp, err := provider.PredictMultimodal(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Say hello in one word.",
			},
		},
	})
	if err != nil {
		t.Fatalf("PredictMultimodal failed: %v", err)
	}

	if resp.Content == "" {
		t.Fatal("expected non-empty response content")
	}
	t.Logf("Response: %s", resp.Content)
}

func TestBedrock_PredictStream_Fallback(t *testing.T) {
	provider := bedrockTestProvider(t)
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

	if lastChunk.FinishReason == nil {
		t.Fatal("expected finish reason in final chunk")
	}
}

func TestBedrock_ErrorOnInvalidModel(t *testing.T) {
	if !hasAWSCredentials() {
		t.Skip("no AWS credentials available, skipping Bedrock integration test")
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-west-2"
	}

	ctx := context.Background()
	cred, err := credentials.NewAWSCredential(ctx, region)
	if err != nil {
		t.Fatalf("failed to create AWS credential: %v", err)
	}

	provider := NewProviderWithCredential(
		"bedrock-test", "anthropic.nonexistent-model-v99:0",
		credentials.BedrockEndpoint(region),
		providers.ProviderDefaults{MaxTokens: 100},
		false, cred, "bedrock", nil,
	)

	_, err = provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid model, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestBedrock_CostCalculation(t *testing.T) {
	provider := bedrockTestProvider(t)
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
