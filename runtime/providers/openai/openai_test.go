package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestNewOpenAIProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
		Pricing: providers.Pricing{
			InputCostPer1K:  0.01,
			OutputCostPer1K: 0.03,
		},
	}

	provider := NewOpenAIProvider("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-openai" {
		t.Errorf("Expected ID 'test-openai', got '%s'", provider.ID())
	}

	if provider.model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", provider.model)
	}

	if provider.baseURL != "https://api.openai.com/v1" {
		t.Error("BaseURL mismatch")
	}

	if provider.defaults.Temperature != 0.7 {
		t.Error("Temperature default mismatch")
	}
}

func TestOpenAIProvider_ID(t *testing.T) {
	ids := []string{"openai-gpt4", "openai-gpt-3.5", "custom-openai"}

	for _, id := range ids {
		provider := NewOpenAIProvider(id, "model", "url", providers.ProviderDefaults{}, false)
		if provider.ID() != id {
			t.Errorf("Expected ID '%s', got '%s'", id, provider.ID())
		}
	}
}

func TestOpenAIProvider_Cost(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01, // $0.01 per 1K input tokens
		OutputCostPer1K: 0.03, // $0.03 per 1K output tokens
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewOpenAIProvider("test", "gpt-4", "url", defaults, false)

	// Test with 1000 input and 1000 output tokens
	breakdown := provider.CalculateCost(1000, 1000, 0)
	expected := 0.01 + 0.03 // $0.04 total

	if breakdown.TotalCost != expected {
		t.Errorf("Expected cost %.4f, got %.4f", expected, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_Cost_LargeTokenCounts(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewOpenAIProvider("test", "gpt-4", "url", defaults, false)

	// Test with 10,000 tokens
	breakdown := provider.CalculateCost(10000, 5000, 0)
	// 10,000 input = 10 * $0.01 = $0.10
	// 5,000 output = 5 * $0.03 = $0.15
	expected := 0.10 + 0.15 // $0.25

	if breakdown.TotalCost != expected {
		t.Errorf("Expected cost %.4f, got %.4f", expected, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_CostBreakdown(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewOpenAIProvider("test", "gpt-4", "url", defaults, false)

	breakdown := provider.CalculateCost(1000, 500, 0)

	if breakdown.InputTokens != 1000 {
		t.Errorf("Expected 1000 input tokens, got %d", breakdown.InputTokens)
	}

	if breakdown.OutputTokens != 500 {
		t.Errorf("Expected 500 output tokens, got %d", breakdown.OutputTokens)
	}

	expectedInputCost := 0.01   // 1000 tokens = 1 * $0.01
	expectedOutputCost := 0.015 // 500 tokens = 0.5 * $0.03

	if breakdown.InputCostUSD != expectedInputCost {
		t.Errorf("Expected input cost %.4f, got %.4f", expectedInputCost, breakdown.InputCostUSD)
	}

	if breakdown.OutputCostUSD != expectedOutputCost {
		t.Errorf("Expected output cost %.4f, got %.4f", expectedOutputCost, breakdown.OutputCostUSD)
	}

	expectedTotal := expectedInputCost + expectedOutputCost
	if breakdown.TotalCost != expectedTotal {
		t.Errorf("Expected total cost %.4f, got %.4f", expectedTotal, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_CostBreakdownWithCachedTokens(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewOpenAIProvider("test", "gpt-4", "url", defaults, false)

	// 1000 input (total), 500 output, 200 cached
	// Cached tokens are subtracted from input tokens: 1000 - 200 = 800 regular input
	breakdown := provider.CalculateCost(1000, 500, 200)

	// InputTokens field contains only non-cached input tokens
	if breakdown.InputTokens != 800 {
		t.Errorf("Expected 800 input tokens (1000 - 200 cached), got %d", breakdown.InputTokens)
	}

	if breakdown.OutputTokens != 500 {
		t.Error("OutputTokens mismatch")
	}

	if breakdown.CachedTokens != 200 {
		t.Error("CachedTokens mismatch")
	}

	// Cached tokens cost 50% of regular input tokens
	expectedCachedCost := 0.001 // 200 * 0.01 / 1000 * 0.5 = 0.001
	// Input cost is for 800 tokens only
	expectedInputCost := 0.008  // 800 * 0.01 / 1000 = 0.008
	expectedOutputCost := 0.015 // 500 * 0.03 / 1000 = 0.015

	if breakdown.CachedCostUSD != expectedCachedCost {
		t.Errorf("Expected cached cost %.4f, got %.4f", expectedCachedCost, breakdown.CachedCostUSD)
	}

	if breakdown.InputCostUSD != expectedInputCost {
		t.Errorf("Expected input cost %.4f, got %.4f", expectedInputCost, breakdown.InputCostUSD)
	}

	// Total should include all costs
	expectedTotal := expectedInputCost + expectedCachedCost + expectedOutputCost
	if breakdown.TotalCost != expectedTotal {
		t.Errorf("Expected total cost %.4f, got %.4f", expectedTotal, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_CostBreakdown_ZeroTokens(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Pricing: providers.Pricing{
			InputCostPer1K:  0.01,
			OutputCostPer1K: 0.03,
		},
	}

	provider := NewOpenAIProvider("test", "gpt-4", "url", defaults, false)

	breakdown := provider.CalculateCost(0, 0, 0)

	if breakdown.TotalCost != 0.0 {
		t.Errorf("Expected zero cost, got %.4f", breakdown.TotalCost)
	}
}

func TestProviderDefaults_Structure(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.8,
		TopP:        0.95,
		MaxTokens:   2000,
		Pricing: providers.Pricing{
			InputCostPer1K:  0.005,
			OutputCostPer1K: 0.015,
		},
	}

	if defaults.Temperature != 0.8 {
		t.Error("Temperature mismatch")
	}

	if defaults.TopP != 0.95 {
		t.Error("TopP mismatch")
	}

	if defaults.MaxTokens != 2000 {
		t.Error("MaxTokens mismatch")
	}

	if defaults.Pricing.InputCostPer1K != 0.005 {
		t.Error("Input pricing mismatch")
	}
}

func TestOpenAIProvider_DifferentModels(t *testing.T) {
	models := []string{"gpt-4", "gpt-4-turbo", "gpt-3.5-turbo", "gpt-4o"}

	for _, model := range models {
		provider := NewOpenAIProvider("test", model, "url", providers.ProviderDefaults{}, false)
		if provider.model != model {
			t.Errorf("Model mismatch for %s", model)
		}
	}
}

func TestOpenAIProvider_DifferentBaseURLs(t *testing.T) {
	urls := []string{
		"https://api.openai.com/v1",
		"https://custom.openai.com/v1",
		"http://localhost:8080/v1",
	}

	for _, url := range urls {
		provider := NewOpenAIProvider("test", "gpt-4", url, providers.ProviderDefaults{}, false)
		if provider.baseURL != url {
			t.Errorf("BaseURL mismatch for %s", url)
		}
	}
}

func TestOpenAIRequest_Structure(t *testing.T) {
	seed := 42
	req := openAIRequest{
		Model: "gpt-4",
		Messages: []openAIMessage{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
		Seed:        &seed,
	}

	if req.Model != "gpt-4" {
		t.Error("Model mismatch")
	}

	if len(req.Messages) != 1 {
		t.Error("Messages count mismatch")
	}

	if req.Temperature != 0.7 {
		t.Error("Temperature mismatch")
	}

	if req.Seed == nil || *req.Seed != 42 {
		t.Error("Seed mismatch")
	}
}

func TestOpenAIMessage_Structure(t *testing.T) {
	msg := openAIMessage{
		Role:    "assistant",
		Content: "Response text",
	}

	if msg.Role != "assistant" {
		t.Error("Role mismatch")
	}

	if msg.Content != "Response text" {
		t.Error("Content mismatch")
	}
}

func TestOpenAIResponse_Structure(t *testing.T) {
	resp := openAIResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
		Choices: []openAIChoice{
			{
				Index: 0,
				Message: openAIMessage{
					Role:    "assistant",
					Content: "Response",
				},
				FinishReason: "stop",
			},
		},
		Usage: openAIUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}

	if resp.ID != "chatcmpl-123" {
		t.Error("ID mismatch")
	}

	if len(resp.Choices) != 1 {
		t.Error("Choices count mismatch")
	}

	if resp.Usage.PromptTokens != 100 {
		t.Error("PromptTokens mismatch")
	}

	if resp.Usage.CompletionTokens != 50 {
		t.Error("CompletionTokens mismatch")
	}
}

func TestOpenAIUsage_WithCachedTokens(t *testing.T) {
	usage := openAIUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		PromptTokensDetails: &openAIPromptDetails{
			CachedTokens: 200,
		},
	}

	if usage.PromptTokensDetails == nil {
		t.Fatal("Expected PromptTokensDetails")
	}

	if usage.PromptTokensDetails.CachedTokens != 200 {
		t.Error("CachedTokens mismatch")
	}
}

func TestOpenAIError_Structure(t *testing.T) {
	err := openAIError{
		Message: "Rate limit exceeded",
		Type:    "rate_limit_error",
		Code:    "rate_limit",
	}

	if err.Message != "Rate limit exceeded" {
		t.Error("Message mismatch")
	}

	if err.Type != "rate_limit_error" {
		t.Error("Type mismatch")
	}

	if err.Code != "rate_limit" {
		t.Error("Code mismatch")
	}
}
