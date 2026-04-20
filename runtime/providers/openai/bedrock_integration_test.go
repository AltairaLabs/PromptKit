//go:build integration

// Bedrock OpenAI integration tests. Exercises the openai provider
// against AWS Bedrock's gpt-oss partner endpoint: standard Bedrock URL
// shape (/model/{id}/invoke), SigV4 auth via the AWS credential chain,
// OpenAI Chat Completions request and response JSON.
//
// Run locally:
//
//	export AWS_PROFILE=<profile-with-bedrock-access>
//	export AWS_REGION=us-west-2
//	export BEDROCK_OPENAI_MODEL=openai.gpt-oss-20b-1:0
//	go test -tags=integration ./runtime/providers/openai/... -run BedrockOpenAI -v
//
// Tests skip when AWS credentials are unavailable. Live calls additionally
// require Bedrock model access for the chosen model in the chosen region.
package openai

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func bedrockRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	return "us-west-2"
}

func bedrockOpenAIModel() string {
	if m := os.Getenv("BEDROCK_OPENAI_MODEL"); m != "" {
		return m
	}
	return "openai.gpt-oss-20b-1:0"
}

func skipIfNoAWS(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := credentials.NewAWSCredential(ctx, bedrockRegion()); err != nil {
		t.Skipf("AWS credentials not available: %v", err)
	}
}

func bedrockOpenAITestProvider(t *testing.T) *Provider {
	t.Helper()
	skipIfNoAWS(t)

	ctx := context.Background()
	cred, err := credentials.NewAWSCredential(ctx, bedrockRegion())
	if err != nil {
		t.Fatalf("failed to create AWS credential: %v", err)
	}

	return NewProviderFromConfig(&ProviderConfig{
		ID:       "bedrock-openai-test",
		Model:    bedrockOpenAIModel(),
		BaseURL:  "", // exercise the factory's PlatformConfig.Region URL derivation
		Platform: "bedrock",
		PlatformConfig: &providers.PlatformConfig{
			Type:   "bedrock",
			Region: bedrockRegion(),
		},
		Credential: cred,
		Defaults: providers.ProviderDefaults{
			MaxTokens:   256,
			Temperature: 0.1,
			Pricing: providers.Pricing{
				InputCostPer1K:  0.00015,
				OutputCostPer1K: 0.0006,
			},
		},
	})
}

func bedrockOpenAITestToolProvider(t *testing.T) *ToolProvider {
	t.Helper()
	return &ToolProvider{Provider: bedrockOpenAITestProvider(t)}
}

func TestBedrockOpenAI_BaseURLConstruction(t *testing.T) {
	skipIfNoAWS(t)

	p := bedrockOpenAITestProvider(t)
	want := credentials.BedrockEndpoint(bedrockRegion())
	if p.baseURL != want {
		t.Fatalf("baseURL = %q, want %q", p.baseURL, want)
	}
}

func TestBedrockOpenAI_Predict(t *testing.T) {
	provider := bedrockOpenAITestProvider(t)
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

func TestBedrockOpenAI_PredictWithTools(t *testing.T) {
	provider := bedrockOpenAITestToolProvider(t)
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

// TestBedrockOpenAI_PredictStream_Fallback exercises the single-chunk
// fallback. Bedrock's binary event-stream is not yet wired for openai;
// PredictStream runs Predict and emits one terminal chunk so callers
// using the streaming interface still get content + finish reason +
// cost on the channel.
func TestBedrockOpenAI_PredictStream_Fallback(t *testing.T) {
	provider := bedrockOpenAITestProvider(t)
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
	if chunkCount != 1 {
		t.Errorf("Bedrock fallback expected exactly 1 chunk, got %d", chunkCount)
	}
	if lastChunk.Content == "" {
		t.Fatal("expected non-empty content in terminal chunk")
	}
	if lastChunk.FinishReason == nil {
		t.Fatal("expected finish reason in terminal chunk")
	}
	t.Logf("Terminal chunk: %s (finish=%s)", lastChunk.Content, *lastChunk.FinishReason)
}

func TestBedrockOpenAI_ErrorOnInvalidModel(t *testing.T) {
	skipIfNoAWS(t)

	ctx := context.Background()
	cred, err := credentials.NewAWSCredential(ctx, bedrockRegion())
	if err != nil {
		t.Fatalf("failed to create AWS credential: %v", err)
	}

	provider := NewProviderFromConfig(&ProviderConfig{
		ID:       "bedrock-openai-test-bad",
		Model:    "openai.model-that-does-not-exist-xyz",
		Platform: "bedrock",
		PlatformConfig: &providers.PlatformConfig{
			Type: "bedrock", Region: bedrockRegion(),
		},
		Credential: cred,
		Defaults:   providers.ProviderDefaults{MaxTokens: 100},
	})

	_, err = provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid model, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestBedrockOpenAI_CostCalculation(t *testing.T) {
	provider := bedrockOpenAITestProvider(t)
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
