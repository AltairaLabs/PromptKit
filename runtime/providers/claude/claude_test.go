package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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

func TestNewProviderWithCredential(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.8,
		TopP:        0.95,
		MaxTokens:   2000,
	}

	t.Run("with credential", func(t *testing.T) {
		cred := &mockCredential{credType: "api_key"}
		provider := NewProviderWithCredential("test-claude", "claude-3-5-sonnet", "https://api.anthropic.com", defaults, false, cred, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.ID() != "test-claude" {
			t.Errorf("Expected ID 'test-claude', got '%s'", provider.ID())
		}

		if provider.model != "claude-3-5-sonnet" {
			t.Errorf("Expected model 'claude-3-5-sonnet', got '%s'", provider.model)
		}

		// baseURL should be normalized to include /v1 for Anthropic API
		if provider.baseURL != "https://api.anthropic.com/v1" {
			t.Errorf("BaseURL mismatch: expected 'https://api.anthropic.com/v1', got '%s'", provider.baseURL)
		}

		if provider.credential == nil {
			t.Error("Expected credential to be set")
		}
	})

	t.Run("with nil credential", func(t *testing.T) {
		provider := NewProviderWithCredential("test-claude", "claude-3-5-sonnet", "https://api.anthropic.com", defaults, false, nil, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.credential != nil {
			t.Error("Expected credential to be nil")
		}
	})
}

// mockCredential implements providers.Credential for testing
type mockCredential struct {
	credType string
}

func (m *mockCredential) Type() string { return m.credType }
func (m *mockCredential) Apply(_ context.Context, _ *http.Request) error {
	return nil
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

// ============================================================================
// Bedrock-specific Tests
// ============================================================================

func TestIsBedrock(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		expected bool
	}{
		{"bedrock platform", "bedrock", true},
		{"empty platform", "", false},
		{"direct platform", "direct", false},
		{"other platform", "vertex", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &Provider{platform: tt.platform}
			if got := provider.isBedrock(); got != tt.expected {
				t.Errorf("isBedrock() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMessagesURL_Direct(t *testing.T) {
	provider := NewProvider("test", "claude-3-5-sonnet-20241022", "https://api.anthropic.com", providers.ProviderDefaults{}, false)
	url := provider.messagesURL()
	expected := "https://api.anthropic.com/v1/messages"
	if url != expected {
		t.Errorf("messagesURL() = %q, want %q", url, expected)
	}
}

func TestMessagesURL_Bedrock(t *testing.T) {
	provider := &Provider{
		model:    "anthropic.claude-3-5-haiku-20241022-v1:0",
		baseURL:  "https://bedrock-runtime.us-east-1.amazonaws.com",
		platform: "bedrock",
	}
	url := provider.messagesURL()
	expected := "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-haiku-20241022-v1:0/invoke"
	if url != expected {
		t.Errorf("messagesURL() = %q, want %q", url, expected)
	}
}

func TestMarshalBedrockRequest(t *testing.T) {
	provider := &Provider{platform: "bedrock"}

	t.Run("basic request", func(t *testing.T) {
		req := claudeRequest{
			Model:     "should-be-ignored",
			MaxTokens: 1024,
			Messages: []claudeMessage{
				{Role: "user", Content: []claudeContentBlock{{Type: "text", Text: "hello"}}},
			},
		}

		data, err := provider.marshalBedrockRequest(&req)
		if err != nil {
			t.Fatalf("marshalBedrockRequest failed: %v", err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		// Must have anthropic_version
		if m["anthropic_version"] != "bedrock-2023-05-31" {
			t.Errorf("anthropic_version = %v, want 'bedrock-2023-05-31'", m["anthropic_version"])
		}

		// Must NOT have model field
		if _, hasModel := m["model"]; hasModel {
			t.Error("Bedrock request should not contain 'model' field")
		}

		// Must have max_tokens
		if m["max_tokens"] != float64(1024) {
			t.Errorf("max_tokens = %v, want 1024", m["max_tokens"])
		}
	})

	t.Run("with optional fields", func(t *testing.T) {
		req := claudeRequest{
			MaxTokens:   512,
			Messages:    []claudeMessage{},
			System:      []claudeContentBlock{{Type: "text", Text: "system prompt"}},
			Temperature: 0.7,
			TopP:        0.9,
		}

		data, err := provider.marshalBedrockRequest(&req)
		if err != nil {
			t.Fatalf("marshalBedrockRequest failed: %v", err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if _, hasSystem := m["system"]; !hasSystem {
			t.Error("expected 'system' field when system prompt is set")
		}
		if _, hasTemp := m["temperature"]; !hasTemp {
			t.Error("expected 'temperature' field when temperature is set")
		}
		if _, hasTopP := m["top_p"]; !hasTopP {
			t.Error("expected 'top_p' field when top_p is set")
		}
	})

	t.Run("zero optional fields omitted", func(t *testing.T) {
		req := claudeRequest{
			MaxTokens: 256,
			Messages:  []claudeMessage{},
		}

		data, err := provider.marshalBedrockRequest(&req)
		if err != nil {
			t.Fatalf("marshalBedrockRequest failed: %v", err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if _, hasSystem := m["system"]; hasSystem {
			t.Error("should not contain 'system' when empty")
		}
		if _, hasTemp := m["temperature"]; hasTemp {
			t.Error("should not contain 'temperature' when zero")
		}
		if _, hasTopP := m["top_p"]; hasTopP {
			t.Error("should not contain 'top_p' when zero")
		}
	})
}

func TestCheckBedrockBodyError_NoError(t *testing.T) {
	body := []byte(`{"id":"msg_123","type":"message","content":[{"type":"text","text":"Hello"}]}`)
	err := checkBedrockBodyError(body)
	if err != nil {
		t.Errorf("expected nil error for normal response, got: %v", err)
	}
}

func TestCheckBedrockBodyError_Exception(t *testing.T) {
	body := []byte(`{"__type":"UnknownOperationException","Message":"Unknown operation: InvokeModel"}`)
	err := checkBedrockBodyError(body)
	if err == nil {
		t.Fatal("expected error for exception response, got nil")
	}
	if !strings.Contains(err.Error(), "UnknownOperationException") {
		t.Errorf("error should mention exception type, got: %v", err)
	}
}

func TestCheckBedrockBodyError_NonExceptionJSON(t *testing.T) {
	// JSON that doesn't contain "Exception" keyword — should not parse
	body := []byte(`{"type":"message","content":[{"type":"text","text":"ok"}]}`)
	err := checkBedrockBodyError(body)
	if err != nil {
		t.Errorf("expected nil for normal JSON without Exception, got: %v", err)
	}
}

func TestParseBedrockHTTPError_StructuredMessage(t *testing.T) {
	body := []byte(`{"message":"Invocation of model ID anthropic.claude-3-5-haiku-20241022-v1:0 with on-demand throughput isn't supported."}`)
	err := parseBedrockHTTPError(400, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "bedrock error") {
		t.Errorf("error should be prefixed with 'bedrock error', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "on-demand throughput") {
		t.Errorf("error should contain the original message, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "400") {
		t.Errorf("error should contain HTTP status code, got: %s", errMsg)
	}
}

func TestParseBedrockHTTPError_RawFallback(t *testing.T) {
	body := []byte(`not json`)
	err := parseBedrockHTTPError(500, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not json") {
		t.Errorf("should fall back to raw body, got: %s", err.Error())
	}
}

func TestApplyAuth_WithCredential(t *testing.T) {
	applied := false
	cred := &applyTrackingCredential{onApply: func() { applied = true }}
	provider := &Provider{credential: cred}

	req, _ := http.NewRequest("POST", "https://example.com", nil)
	err := provider.applyAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("applyAuth failed: %v", err)
	}
	if !applied {
		t.Error("expected credential.Apply() to be called")
	}
}

func TestApplyAuth_FallbackAPIKey(t *testing.T) {
	provider := &Provider{apiKey: "sk-test-key"}

	req, _ := http.NewRequest("POST", "https://example.com", nil)
	err := provider.applyAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("applyAuth failed: %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "sk-test-key" {
		t.Errorf("X-API-Key = %q, want 'sk-test-key'", got)
	}
}

func TestNormalizeBaseURL_Bedrock(t *testing.T) {
	// Bedrock URLs should NOT be normalized (no /v1 appended)
	bedrockURL := "https://bedrock-runtime.us-east-1.amazonaws.com"
	result := normalizeBaseURL(bedrockURL)
	if result != bedrockURL {
		t.Errorf("normalizeBaseURL(%q) = %q, want unchanged URL", bedrockURL, result)
	}
}

// applyTrackingCredential tracks whether Apply was called
type applyTrackingCredential struct {
	onApply func()
}

func (c *applyTrackingCredential) Type() string { return "tracking" }
func (c *applyTrackingCredential) Apply(_ context.Context, _ *http.Request) error {
	c.onApply()
	return nil
}
