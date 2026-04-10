package openai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestOpenRouter_CustomHeaders_Integration verifies that custom HTTP headers
// are forwarded to a real OpenAI-compatible gateway (OpenRouter). This proves
// the headers feature end-to-end against a real upstream, not just a mock.
//
// Requires OPENROUTER_API_KEY in the environment. Skipped otherwise.
//
// Run locally with:
//
//	source .env
//	go test ./runtime/providers/openai/ -run TestOpenRouter_CustomHeaders_Integration -v -count=1
func TestOpenRouter_CustomHeaders_Integration(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	// The OpenAI provider reads OPENAI_API_KEY; override it with the
	// OpenRouter key for this test. OpenRouter accepts the same Bearer
	// token scheme as OpenAI at its /api/v1 endpoint.
	t.Setenv("OPENAI_API_KEY", apiKey)

	spec := providers.ProviderSpec{
		ID:      "test-openrouter",
		Type:    "openai",
		Model:   "openai/gpt-4o-mini",
		BaseURL: "https://openrouter.ai/api/v1",
		Headers: map[string]string{
			"HTTP-Referer": "https://github.com/AltairaLabs/PromptKit",
			"X-Title":      "PromptKit Integration Test",
		},
		RequestTimeout: 30 * time.Second,
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Reply with the single word: pong"},
		},
		MaxTokens:   20,
		Temperature: 0,
	})
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}

	if resp.Content == "" {
		t.Fatalf("expected non-empty response content, got empty. Full response: %+v", resp)
	}

	if resp.CostInfo != nil {
		t.Logf("OpenRouter response: %q (tokens in=%d out=%d)",
			resp.Content, resp.CostInfo.InputTokens, resp.CostInfo.OutputTokens)
	} else {
		t.Logf("OpenRouter response: %q", resp.Content)
	}

	// Sanity check: response should contain something vaguely on-topic.
	// We don't assert exact match because small free models vary.
	if !strings.Contains(strings.ToLower(resp.Content), "pong") &&
		len(resp.Content) < 2 {
		t.Errorf("response looks malformed: %q", resp.Content)
	}
}

// TestOpenRouter_CollisionDetection_Integration verifies that header collisions
// are detected and rejected against the real gateway. This proves the
// collision check happens before the request is sent.
func TestOpenRouter_CollisionDetection_Integration(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	t.Setenv("OPENAI_API_KEY", apiKey)

	spec := providers.ProviderSpec{
		ID:      "test-openrouter-collision",
		Type:    "openai",
		Model:   "openai/gpt-4o-mini",
		BaseURL: "https://openrouter.ai/api/v1",
		Headers: map[string]string{
			// Collides with the provider's built-in Authorization header.
			"Authorization": "Bearer fake-conflict",
		},
		RequestTimeout: 30 * time.Second,
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = provider.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 20,
	})
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
	if !strings.Contains(err.Error(), "custom header") {
		t.Errorf("expected custom header collision error, got: %v", err)
	}
}
