package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.9,
		TopP:        0.95,
		MaxTokens:   2048,
		Pricing: providers.Pricing{
			InputCostPer1K:  0.00125,
			OutputCostPer1K: 0.005,
		},
	}

	provider := NewProvider("test-gemini", "gemini-1.5-pro", "https://generativelanguage.googleapis.com/v1beta", defaults, false)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-gemini" {
		t.Errorf("Expected ID 'test-gemini', got '%s'", provider.ID())
	}

	if provider.Model() != "gemini-1.5-pro" {
		t.Errorf("Expected model 'gemini-1.5-pro', got '%s'", provider.Model())
	}

	if provider.baseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Error("BaseURL mismatch")
	}

	if provider.defaults.Temperature != 0.9 {
		t.Error("Temperature default mismatch")
	}
}

func TestNewProviderWithCredential(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.9,
		TopP:        0.95,
		MaxTokens:   2048,
	}

	t.Run("with APIKeyCredential", func(t *testing.T) {
		cred := &mockAPIKeyCredential{apiKey: "test-api-key"}
		provider := NewProviderWithCredential("test-gemini", "gemini-1.5-pro", "https://api.example.com", defaults, false, cred, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.ID() != "test-gemini" {
			t.Errorf("Expected ID 'test-gemini', got '%s'", provider.ID())
		}

		if provider.Model() != "gemini-1.5-pro" {
			t.Errorf("Expected model 'gemini-1.5-pro', got '%s'", provider.Model())
		}

		if provider.apiKey != "test-api-key" {
			t.Errorf("Expected ApiKey 'test-api-key', got '%s'", provider.apiKey)
		}

		if provider.credential == nil {
			t.Error("Expected credential to be set")
		}
	})

	t.Run("with nil credential", func(t *testing.T) {
		provider := NewProviderWithCredential("test-gemini", "gemini-1.5-pro", "https://api.example.com", defaults, false, nil, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.apiKey != "" {
			t.Errorf("Expected empty ApiKey, got '%s'", provider.apiKey)
		}
	})

	t.Run("with non-APIKey credential", func(t *testing.T) {
		cred := &mockOtherCredential{}
		provider := NewProviderWithCredential("test-gemini", "gemini-1.5-pro", "https://api.example.com", defaults, false, cred, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		// Should not extract API key from non-api_key credential
		if provider.apiKey != "" {
			t.Errorf("Expected empty ApiKey for non-APIKey credential, got '%s'", provider.apiKey)
		}

		if provider.credential == nil {
			t.Error("Expected credential to be set")
		}
	})
}

// mockAPIKeyCredential implements providers.Credential for testing
type mockAPIKeyCredential struct {
	apiKey string
}

func (m *mockAPIKeyCredential) Type() string { return "api_key" }
func (m *mockAPIKeyCredential) Apply(_ context.Context, _ *http.Request) error {
	return nil
}
func (m *mockAPIKeyCredential) APIKey() string { return m.apiKey }

// mockOtherCredential implements providers.Credential for testing non-APIKey credentials
type mockOtherCredential struct{}

func (m *mockOtherCredential) Type() string { return "other" }
func (m *mockOtherCredential) Apply(_ context.Context, _ *http.Request) error {
	return nil
}

func TestGeminiProvider_ID(t *testing.T) {
	ids := []string{"gemini-pro", "gemini-flash", "custom-gemini"}

	for _, id := range ids {
		provider := NewProvider(id, "model", "url", providers.ProviderDefaults{}, false)
		if provider.ID() != id {
			t.Errorf("Expected ID '%s', got '%s'", id, provider.ID())
		}
	}
}

func TestGeminiProvider_Cost(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.00125, // Gemini Pro pricing
		OutputCostPer1K: 0.005,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gemini-1.5-pro", "url", defaults, false)

	// Test with 1000 input and 1000 output tokens
	breakdown := provider.CalculateCost(1000, 1000, 0)
	expected := 0.00125 + 0.005 // $0.00625 total

	if breakdown.TotalCost != expected {
		t.Errorf("Expected cost %.5f, got %.5f", expected, breakdown.TotalCost)
	}
}

func TestGeminiProvider_Cost_FlashPricing(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.000075, // Flash pricing
		OutputCostPer1K: 0.0003,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gemini-1.5-flash", "url", defaults, false)

	breakdown := provider.CalculateCost(1000, 1000, 0)
	expected := 0.000075 + 0.0003 // $0.000375 total

	tolerance := 0.0000001
	if breakdown.TotalCost < expected-tolerance || breakdown.TotalCost > expected+tolerance {
		t.Errorf("Expected cost %.6f, got %.6f", expected, breakdown.TotalCost)
	}
}

func TestGeminiProvider_CostBreakdown(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.00125,
		OutputCostPer1K: 0.005,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gemini-1.5-pro", "url", defaults, false)

	breakdown := provider.CalculateCost(1000, 500, 0)

	if breakdown.InputTokens != 1000 {
		t.Errorf("Expected 1000 input tokens, got %d", breakdown.InputTokens)
	}

	if breakdown.OutputTokens != 500 {
		t.Errorf("Expected 500 output tokens, got %d", breakdown.OutputTokens)
	}

	expectedInputCost := 0.00125 // 1000 tokens = 1 * $0.00125
	expectedOutputCost := 0.0025 // 500 tokens = 0.5 * $0.005

	if breakdown.InputCostUSD != expectedInputCost {
		t.Errorf("Expected input cost %.5f, got %.5f", expectedInputCost, breakdown.InputCostUSD)
	}

	if breakdown.OutputCostUSD != expectedOutputCost {
		t.Errorf("Expected output cost %.4f, got %.4f", expectedOutputCost, breakdown.OutputCostUSD)
	}

	expectedTotal := expectedInputCost + expectedOutputCost
	if breakdown.TotalCost != expectedTotal {
		t.Errorf("Expected total cost %.5f, got %.5f", expectedTotal, breakdown.TotalCost)
	}
}

func TestGeminiProvider_CostBreakdownWithCachedTokens(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.00125,
		OutputCostPer1K: 0.005,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gemini-1.5-pro", "url", defaults, false)

	// 1000 input (total), 500 output, 200 cached
	// Gemini cached tokens cost 50% of input tokens
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

	// Cached tokens cost 50% of regular input tokens (Gemini pricing)
	expectedCachedCost := 0.000125 // 200 * 0.00125 / 1000 * 0.5 = 0.000125
	// Input cost is for 800 tokens only
	expectedInputCost := 0.001   // 800 * 0.00125 / 1000 = 0.001
	expectedOutputCost := 0.0025 // 500 * 0.005 / 1000 = 0.0025

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

func TestGeminiProvider_CostBreakdown_ZeroTokens(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Pricing: providers.Pricing{
			InputCostPer1K:  0.00125,
			OutputCostPer1K: 0.005,
		},
	}

	provider := NewProvider("test", "gemini-1.5-pro", "url", defaults, false)

	breakdown := provider.CalculateCost(0, 0, 0)

	if breakdown.TotalCost != 0.0 {
		t.Errorf("Expected zero cost, got %.4f", breakdown.TotalCost)
	}
}

func TestGeminiProvider_DifferentModels(t *testing.T) {
	models := []string{
		"gemini-1.5-pro",
		"gemini-2.5-pro",
		"gemini-1.5-flash",
		"gemini-2.5-flash",
		"gemini-pro",
	}

	for _, model := range models {
		provider := NewProvider("test", model, "url", providers.ProviderDefaults{}, false)
		if provider.Model() != model {
			t.Errorf("Model mismatch for %s", model)
		}
	}
}

func TestGeminiRequest_Structure(t *testing.T) {
	req := geminiRequest{
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: "Hello"}},
			},
		},
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: "You are helpful"}},
		},
		GenerationConfig: geminiGenConfig{
			Temperature:     0.7,
			TopP:            0.9,
			MaxOutputTokens: 1000,
		},
		SafetySettings: []geminiSafety{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_ONLY_HIGH"},
		},
	}

	if len(req.Contents) != 1 {
		t.Error("Contents count mismatch")
	}

	if req.SystemInstruction == nil {
		t.Error("Expected system instruction")
	}

	if req.GenerationConfig.Temperature != 0.7 {
		t.Error("Temperature mismatch")
	}

	if len(req.SafetySettings) != 1 {
		t.Error("Safety settings count mismatch")
	}
}

func TestGeminiContent_Structure(t *testing.T) {
	content := geminiContent{
		Role:  "model",
		Parts: []geminiPart{{Text: "Response text"}},
	}

	if content.Role != "model" {
		t.Error("Role mismatch")
	}

	if len(content.Parts) != 1 {
		t.Error("Parts count mismatch")
	}

	if content.Parts[0].Text != "Response text" {
		t.Error("Part text mismatch")
	}
}

func TestGeminiGenConfig_Structure(t *testing.T) {
	config := geminiGenConfig{
		Temperature:     0.8,
		TopP:            0.95,
		MaxOutputTokens: 2048,
	}

	if config.Temperature != 0.8 {
		t.Error("Temperature mismatch")
	}

	if config.TopP != 0.95 {
		t.Error("TopP mismatch")
	}

	if config.MaxOutputTokens != 2048 {
		t.Error("MaxOutputTokens mismatch")
	}
}

func TestGeminiResponse_Structure(t *testing.T) {
	resp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: "Response"}},
				},
				FinishReason: "STOP",
				Index:        0,
			},
		},
		UsageMetadata: &geminiUsage{
			PromptTokenCount:     100,
			CandidatesTokenCount: 50,
			TotalTokenCount:      150,
		},
	}

	if len(resp.Candidates) != 1 {
		t.Error("Candidates count mismatch")
	}

	if resp.UsageMetadata == nil {
		t.Fatal("Expected usage metadata")
	}

	if resp.UsageMetadata.PromptTokenCount != 100 {
		t.Error("PromptTokenCount mismatch")
	}

	if resp.UsageMetadata.CandidatesTokenCount != 50 {
		t.Error("CandidatesTokenCount mismatch")
	}
}

func TestGeminiCandidate_Structure(t *testing.T) {
	candidate := geminiCandidate{
		Content: geminiContent{
			Role:  "model",
			Parts: []geminiPart{{Text: "Content"}},
		},
		FinishReason: "STOP",
		Index:        0,
		SafetyRatings: []geminiSafetyRating{
			{Category: "HARM_CATEGORY_HARASSMENT", Probability: "NEGLIGIBLE"},
		},
	}

	if candidate.FinishReason != "STOP" {
		t.Error("FinishReason mismatch")
	}

	if candidate.Index != 0 {
		t.Error("Index mismatch")
	}

	if len(candidate.SafetyRatings) != 1 {
		t.Error("SafetyRatings count mismatch")
	}
}

func TestGeminiUsage_Structure(t *testing.T) {
	usage := geminiUsage{
		PromptTokenCount:     1000,
		CandidatesTokenCount: 500,
		TotalTokenCount:      1500,
	}

	if usage.PromptTokenCount != 1000 {
		t.Error("PromptTokenCount mismatch")
	}

	if usage.CandidatesTokenCount != 500 {
		t.Error("CandidatesTokenCount mismatch")
	}

	if usage.TotalTokenCount != 1500 {
		t.Error("TotalTokenCount mismatch")
	}
}

func TestGeminiSafety_Structure(t *testing.T) {
	safety := geminiSafety{
		Category:  "HARM_CATEGORY_HARASSMENT",
		Threshold: "BLOCK_ONLY_HIGH",
	}

	if safety.Category != "HARM_CATEGORY_HARASSMENT" {
		t.Error("Category mismatch")
	}

	if safety.Threshold != "BLOCK_ONLY_HIGH" {
		t.Error("Threshold mismatch")
	}
}

func TestGeminiPromptFeedback_Structure(t *testing.T) {
	feedback := geminiPromptFeedback{
		SafetyRatings: []geminiSafetyRating{
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Probability: "LOW"},
		},
		BlockReason: "SAFETY",
	}

	if len(feedback.SafetyRatings) != 1 {
		t.Error("SafetyRatings count mismatch")
	}

	if feedback.BlockReason != "SAFETY" {
		t.Error("BlockReason mismatch")
	}
}

// TestInferMediaTypeFromMIME tests the MIME type inference function
// TestConvertMessagesToGeminiContents_WithParts verifies that messages created using
// AddTextPart() (SDK style) are correctly converted to Gemini format.
// This is a regression test for a bug where convertMessagesToGeminiContents used
// .Content directly instead of GetContent(), which would return empty string for
// messages that store text in Parts rather than the Content field.
func TestConvertMessagesToGeminiContents_WithParts(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() types.Message
		wantText string
	}{
		{
			name: "legacy message with Content field",
			setup: func() types.Message {
				return types.Message{
					Role:    "user",
					Content: "Hello from Content field",
				}
			},
			wantText: "Hello from Content field",
		},
		{
			name: "SDK-style message with AddTextPart",
			setup: func() types.Message {
				msg := types.Message{Role: "user"}
				msg.AddTextPart("Hello from Parts")
				return msg
			},
			wantText: "Hello from Parts",
		},
		{
			name: "SDK-style message with multiple text parts",
			setup: func() types.Message {
				msg := types.Message{Role: "user"}
				msg.AddTextPart("First part. ")
				msg.AddTextPart("Second part.")
				return msg
			},
			wantText: "First part. Second part.",
		},
		{
			name: "assistant message with AddTextPart",
			setup: func() types.Message {
				msg := types.Message{Role: "assistant"}
				msg.AddTextPart("Assistant response")
				return msg
			},
			wantText: "Assistant response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.setup()
			contents := convertMessagesToGeminiContents([]types.Message{msg})

			if len(contents) != 1 {
				t.Fatalf("Expected 1 content, got %d", len(contents))
			}

			if len(contents[0].Parts) != 1 {
				t.Fatalf("Expected 1 part, got %d", len(contents[0].Parts))
			}

			gotText := contents[0].Parts[0].Text
			if gotText != tt.wantText {
				t.Errorf("Text mismatch:\n  got:  %q\n  want: %q", gotText, tt.wantText)
			}
		})
	}
}

// TestInferMediaTypeFromMIME tests the MIME type inference function
func TestInferMediaTypeFromMIME(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     string
	}{
		{
			name:     "image/jpeg",
			mimeType: "image/jpeg",
			want:     "image",
		},
		{
			name:     "image/png",
			mimeType: "image/png",
			want:     "image",
		},
		{
			name:     "image/webp",
			mimeType: "image/webp",
			want:     "image",
		},
		{
			name:     "audio/mp3",
			mimeType: "audio/mp3",
			want:     "audio",
		},
		{
			name:     "audio/wav",
			mimeType: "audio/wav",
			want:     "audio",
		},
		{
			name:     "video/mp4",
			mimeType: "video/mp4",
			want:     "video",
		},
		{
			name:     "video/webm",
			mimeType: "video/webm",
			want:     "video",
		},
		{
			name:     "unknown type",
			mimeType: "application/json",
			want:     "",
		},
		{
			name:     "text type",
			mimeType: "text/plain",
			want:     "",
		},
		{
			name:     "empty string",
			mimeType: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferMediaTypeFromMIME(tt.mimeType)
			if got != tt.want {
				t.Errorf("inferMediaTypeFromMIME(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestApplyResponseFormat(t *testing.T) {
	p := &Provider{}

	t.Run("nil ResponseFormat", func(t *testing.T) {
		req := &geminiRequest{}
		p.applyResponseFormat(req, nil)

		if req.GenerationConfig.ResponseMimeType != "" {
			t.Errorf("Expected empty ResponseMimeType, got %q", req.GenerationConfig.ResponseMimeType)
		}
		if req.GenerationConfig.ResponseSchema != nil {
			t.Error("Expected nil ResponseSchema")
		}
	})

	t.Run("ResponseFormatJSON", func(t *testing.T) {
		req := &geminiRequest{}
		p.applyResponseFormat(req, &providers.ResponseFormat{
			Type: providers.ResponseFormatJSON,
		})

		if req.GenerationConfig.ResponseMimeType != "application/json" {
			t.Errorf("Expected ResponseMimeType 'application/json', got %q", req.GenerationConfig.ResponseMimeType)
		}
		if req.GenerationConfig.ResponseSchema != nil {
			t.Error("Expected nil ResponseSchema for JSON mode")
		}
	})

	t.Run("ResponseFormatJSONSchema with valid schema", func(t *testing.T) {
		schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
		req := &geminiRequest{}
		p.applyResponseFormat(req, &providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: schema,
		})

		if req.GenerationConfig.ResponseMimeType != "application/json" {
			t.Errorf("Expected ResponseMimeType 'application/json', got %q", req.GenerationConfig.ResponseMimeType)
		}
		if req.GenerationConfig.ResponseSchema == nil {
			t.Fatal("Expected non-nil ResponseSchema")
		}
	})

	t.Run("ResponseFormatJSONSchema with empty schema", func(t *testing.T) {
		req := &geminiRequest{}
		p.applyResponseFormat(req, &providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage{},
		})

		if req.GenerationConfig.ResponseMimeType != "application/json" {
			t.Errorf("Expected ResponseMimeType 'application/json', got %q", req.GenerationConfig.ResponseMimeType)
		}
		if req.GenerationConfig.ResponseSchema != nil {
			t.Error("Expected nil ResponseSchema for empty schema")
		}
	})

	t.Run("ResponseFormatJSONSchema with invalid JSON", func(t *testing.T) {
		req := &geminiRequest{}
		p.applyResponseFormat(req, &providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(`{invalid json`),
		})

		if req.GenerationConfig.ResponseMimeType != "application/json" {
			t.Errorf("Expected ResponseMimeType 'application/json', got %q", req.GenerationConfig.ResponseMimeType)
		}
		if req.GenerationConfig.ResponseSchema != nil {
			t.Error("Expected nil ResponseSchema for invalid JSON")
		}
	})

	t.Run("ResponseFormatText", func(t *testing.T) {
		req := &geminiRequest{}
		p.applyResponseFormat(req, &providers.ResponseFormat{
			Type: providers.ResponseFormatText,
		})

		if req.GenerationConfig.ResponseMimeType != "" {
			t.Errorf("Expected empty ResponseMimeType for text format, got %q", req.GenerationConfig.ResponseMimeType)
		}
		if req.GenerationConfig.ResponseSchema != nil {
			t.Error("Expected nil ResponseSchema for text format")
		}
	})
}

func TestGemini_PlatformFieldsStored(t *testing.T) {
	defaults := providers.ProviderDefaults{Temperature: 0.7}
	pc := &providers.PlatformConfig{Region: "us-west-2"}
	cred := &mockAPIKeyCredential{apiKey: "test-key"}

	provider := NewProviderWithCredential("test", "gemini-pro", "https://example.com", defaults, false, cred, "bedrock", pc)

	if provider.platform != "bedrock" {
		t.Errorf("Expected platform 'bedrock', got %q", provider.platform)
	}
	if provider.platformConfig == nil {
		t.Fatal("Expected platformConfig to be set")
	}
	if provider.platformConfig.Region != "us-west-2" {
		t.Errorf("Expected region 'us-west-2', got %q", provider.platformConfig.Region)
	}
}

func TestGemini_PlatformField(t *testing.T) {
	tests := []struct {
		platform string
		isBr     bool
	}{
		{"bedrock", true},
		{"vertex", false},
		{"", false},
	}
	for _, tt := range tests {
		p := &Provider{platform: tt.platform}
		got := p.platform == "bedrock"
		if got != tt.isBr {
			t.Errorf("platform=%q == bedrock: got %v, want %v", tt.platform, got, tt.isBr)
		}
	}
}
