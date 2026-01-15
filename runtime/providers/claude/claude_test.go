package claude

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestNewProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.8,
		TopP:        0.95,
		MaxTokens:   2000,
		Pricing: providers.Pricing{
			InputCostPer1K:  0.003,
			OutputCostPer1K: 0.015,
		},
	}

	provider := NewProvider("test-claude", "claude-3-5-sonnet-20241022", "https://api.anthropic.com", defaults, false)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-claude" {
		t.Errorf("Expected ID 'test-claude', got '%s'", provider.ID())
	}

	if provider.model != "claude-3-5-sonnet-20241022" {
		t.Errorf("Expected model 'claude-3-5-sonnet-20241022', got '%s'", provider.model)
	}

	// baseURL should be normalized to include /v1 for Anthropic API
	if provider.baseURL != "https://api.anthropic.com/v1" {
		t.Errorf("BaseURL mismatch: expected 'https://api.anthropic.com/v1', got '%s'", provider.baseURL)
	}

	if provider.defaults.Temperature != 0.8 {
		t.Error("Temperature default mismatch")
	}
}

func TestClaudeProvider_ID(t *testing.T) {
	ids := []string{"claude-sonnet", "claude-haiku", "custom-claude"}

	for _, id := range ids {
		provider := NewProvider(id, "model", "url", providers.ProviderDefaults{}, false)
		if provider.ID() != id {
			t.Errorf("Expected ID '%s', got '%s'", id, provider.ID())
		}
	}
}

func TestClaudeProvider_Cost(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.003, // $0.003 per 1K input tokens (Sonnet)
		OutputCostPer1K: 0.015, // $0.015 per 1K output tokens (Sonnet)
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "claude-3-5-sonnet-20241022", "url", defaults, false)

	// Test with 1000 input and 1000 output tokens
	breakdown := provider.CalculateCost(1000, 1000, 0)
	expected := 0.003 + 0.015 // $0.018 total

	if breakdown.TotalCost != expected {
		t.Errorf("Expected cost %.4f, got %.4f", expected, breakdown.TotalCost)
	}
}

func TestClaudeProvider_Cost_HaikuPricing(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.001, // Haiku pricing
		OutputCostPer1K: 0.005,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "claude-3-5-haiku-20241022", "url", defaults, false)

	breakdown := provider.CalculateCost(1000, 1000, 0)
	expected := 0.001 + 0.005 // $0.006 total

	if breakdown.TotalCost != expected {
		t.Errorf("Expected cost %.4f, got %.4f", expected, breakdown.TotalCost)
	}
}

func TestClaudeProvider_CostBreakdown(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.003,
		OutputCostPer1K: 0.015,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "claude-3-5-sonnet-20241022", "url", defaults, false)

	breakdown := provider.CalculateCost(1000, 500, 0)

	if breakdown.InputTokens != 1000 {
		t.Errorf("Expected 1000 input tokens, got %d", breakdown.InputTokens)
	}

	if breakdown.OutputTokens != 500 {
		t.Errorf("Expected 500 output tokens, got %d", breakdown.OutputTokens)
	}

	expectedInputCost := 0.003   // 1000 tokens = 1 * $0.003
	expectedOutputCost := 0.0075 // 500 tokens = 0.5 * $0.015

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

func TestClaudeProvider_CostBreakdownWithCachedTokens(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.003,
		OutputCostPer1K: 0.015,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "claude-3-5-sonnet-20241022", "url", defaults, false)

	// 1000 input (total), 500 output, 200 cached
	// Claude cached tokens cost 10% of input tokens
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

	// Cached tokens cost 10% of regular input tokens (Claude pricing)
	expectedCachedCost := 0.00006 // 200 * 0.003 / 1000 * 0.1 = 0.00006
	// Input cost is for 800 tokens only
	expectedInputCost := 0.0024  // 800 * 0.003 / 1000 = 0.0024
	expectedOutputCost := 0.0075 // 500 * 0.015 / 1000 = 0.0075

	// Use tolerance for floating point comparison
	tolerance := 0.0000001
	if breakdown.CachedCostUSD < expectedCachedCost-tolerance || breakdown.CachedCostUSD > expectedCachedCost+tolerance {
		t.Errorf("Expected cached cost %.6f, got %.6f", expectedCachedCost, breakdown.CachedCostUSD)
	}

	if breakdown.InputCostUSD < expectedInputCost-tolerance || breakdown.InputCostUSD > expectedInputCost+tolerance {
		t.Errorf("Expected input cost %.4f, got %.4f", expectedInputCost, breakdown.InputCostUSD)
	}

	// Total should include all costs
	expectedTotal := expectedInputCost + expectedCachedCost + expectedOutputCost
	if breakdown.TotalCost < expectedTotal-tolerance || breakdown.TotalCost > expectedTotal+tolerance {
		t.Errorf("Expected total cost %.6f, got %.6f", expectedTotal, breakdown.TotalCost)
	}
}

func TestClaudeProvider_CostBreakdown_ZeroTokens(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Pricing: providers.Pricing{
			InputCostPer1K:  0.003,
			OutputCostPer1K: 0.015,
		},
	}

	provider := NewProvider("test", "claude-3-5-sonnet-20241022", "url", defaults, false)

	breakdown := provider.CalculateCost(0, 0, 0)

	if breakdown.TotalCost != 0.0 {
		t.Errorf("Expected zero cost, got %.4f", breakdown.TotalCost)
	}
}

func TestClaudeProvider_DifferentModels(t *testing.T) {
	models := []string{
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}

	for _, model := range models {
		provider := NewProvider("test", model, "url", providers.ProviderDefaults{}, false)
		if provider.model != model {
			t.Errorf("Model mismatch for %s", model)
		}
	}
}

func TestClaudeRequest_Structure(t *testing.T) {
	req := claudeRequest{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 1000,
		Messages: []claudeMessage{
			{
				Role: "user",
				Content: []claudeContentBlock{
					{Type: "text", Text: "Hello"},
				},
			},
		},
		System: []claudeContentBlock{
			{Type: "text", Text: "You are helpful"},
		},
		Temperature: 0.7,
		TopP:        0.9,
	}

	if req.Model != "claude-3-5-sonnet-20241022" {
		t.Error("Model mismatch")
	}

	if req.MaxTokens != 1000 {
		t.Error("MaxTokens mismatch")
	}

	if len(req.Messages) != 1 {
		t.Error("Messages count mismatch")
	}

	if len(req.System) != 1 {
		t.Error("System blocks count mismatch")
	}
}

func TestClaudeMessage_Structure(t *testing.T) {
	msg := claudeMessage{
		Role: "assistant",
		Content: []claudeContentBlock{
			{Type: "text", Text: "Response text"},
		},
	}

	if msg.Role != "assistant" {
		t.Error("Role mismatch")
	}

	if len(msg.Content) != 1 {
		t.Error("Content count mismatch")
	}

	if msg.Content[0].Text != "Response text" {
		t.Error("Content text mismatch")
	}
}

func TestClaudeContentBlock_WithCacheControl(t *testing.T) {
	block := claudeContentBlock{
		Type: "text",
		Text: "Cacheable content",
		CacheControl: &claudeCacheControl{
			Type: "ephemeral",
		},
	}

	if block.Type != "text" {
		t.Error("Type mismatch")
	}

	if block.CacheControl == nil {
		t.Fatal("Expected cache control")
	}

	if block.CacheControl.Type != "ephemeral" {
		t.Error("Cache control type mismatch")
	}
}

func TestClaudeResponse_Structure(t *testing.T) {
	resp := claudeResponse{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []claudeContent{
			{Type: "text", Text: "Response"},
		},
		Model:      "claude-3-5-sonnet-20241022",
		StopReason: "end_turn",
		Usage: claudeUsage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	if resp.ID != "msg_123" {
		t.Error("ID mismatch")
	}

	if len(resp.Content) != 1 {
		t.Error("Content count mismatch")
	}

	if resp.Usage.InputTokens != 100 {
		t.Error("InputTokens mismatch")
	}

	if resp.Usage.OutputTokens != 50 {
		t.Error("OutputTokens mismatch")
	}
}

func TestClaudeUsage_WithCachedTokens(t *testing.T) {
	usage := claudeUsage{
		InputTokens:              1000,
		OutputTokens:             500,
		CacheCreationInputTokens: 100,
		CacheReadInputTokens:     200,
	}

	if usage.InputTokens != 1000 {
		t.Error("InputTokens mismatch")
	}

	if usage.CacheCreationInputTokens != 100 {
		t.Error("CacheCreationInputTokens mismatch")
	}

	if usage.CacheReadInputTokens != 200 {
		t.Error("CacheReadInputTokens mismatch")
	}
}

func TestClaudeError_Structure(t *testing.T) {
	err := claudeError{
		Type:    "rate_limit_error",
		Message: "Rate limit exceeded",
	}

	if err.Type != "rate_limit_error" {
		t.Error("Type mismatch")
	}

	if err.Message != "Rate limit exceeded" {
		t.Error("Message mismatch")
	}
}

func TestClaudeContent_DifferentTypes(t *testing.T) {
	textContent := claudeContent{
		Type: "text",
		Text: "Some text",
	}

	if textContent.Type != "text" {
		t.Error("Type mismatch for text content")
	}

	if textContent.Text != "Some text" {
		t.Error("Text mismatch")
	}
}
