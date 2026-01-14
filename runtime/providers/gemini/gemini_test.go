package gemini

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
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

	if provider.Model != "gemini-1.5-pro" {
		t.Errorf("Expected model 'gemini-1.5-pro', got '%s'", provider.Model)
	}

	if provider.BaseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Error("BaseURL mismatch")
	}

	if provider.Defaults.Temperature != 0.9 {
		t.Error("Temperature default mismatch")
	}
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
		if provider.Model != model {
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
