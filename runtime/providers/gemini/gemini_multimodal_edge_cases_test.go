package gemini

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestGeminiProvider_ParseGeminiResponse_PromptBlocked(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)

	respJSON := `{
		"promptFeedback": {
			"blockReason": "SAFETY",
			"safetyRatings": []
		}
	}`

	_, err := provider.parseGeminiResponse([]byte(respJSON))
	if err == nil {
		t.Error("Expected error for blocked prompt")
	}

	if err != nil && err.Error() != "prompt blocked: SAFETY" {
		t.Errorf("Expected 'prompt blocked: SAFETY', got: %v", err)
	}
}

func TestGeminiProvider_ParseGeminiResponse_NoCandidates(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)

	respJSON := `{
		"candidates": []
	}`

	_, err := provider.parseGeminiResponse([]byte(respJSON))
	if err == nil {
		t.Error("Expected error for no candidates")
	}

	if err != nil && err.Error() != "no candidates in response" {
		t.Errorf("Expected 'no candidates in response', got: %v", err)
	}
}

func TestGeminiProvider_ParseGeminiResponse_MaxTokens(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)

	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "MAX_TOKENS"
		}]
	}`

	_, err := provider.parseGeminiResponse([]byte(respJSON))
	if err == nil {
		t.Error("Expected error for MAX_TOKENS")
	}

	if err != nil && err.Error() != "max tokens limit reached" {
		t.Errorf("Expected 'max tokens limit reached', got: %v", err)
	}
}

func TestGeminiProvider_ParseGeminiResponse_Safety(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)

	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "SAFETY"
		}]
	}`

	_, err := provider.parseGeminiResponse([]byte(respJSON))
	if err == nil {
		t.Error("Expected error for SAFETY")
	}

	if err != nil && err.Error() != "response blocked by safety filters" {
		t.Errorf("Expected 'response blocked by safety filters', got: %v", err)
	}
}

func TestGeminiProvider_ParseGeminiResponse_Recitation(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)

	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "RECITATION"
		}]
	}`

	_, err := provider.parseGeminiResponse([]byte(respJSON))
	if err == nil {
		t.Error("Expected error for RECITATION")
	}

	if err != nil && err.Error() != "response blocked due to recitation concerns" {
		t.Errorf("Expected 'response blocked due to recitation concerns', got: %v", err)
	}
}

func TestGeminiProvider_ParseGeminiResponse_UnknownFinishReason(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)

	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "UNKNOWN"
		}]
	}`

	_, err := provider.parseGeminiResponse([]byte(respJSON))
	if err == nil {
		t.Error("Expected error for unknown finish reason with no parts")
	}
}

func TestGeminiProvider_ConvertMediaPartToGemini_URLError(t *testing.T) {
	// Use a URL that will fail quickly (localhost unreachable port)
	url := "http://localhost:1/nonexistent.jpg"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			URL:      &url,
			MIMEType: "image/jpeg",
		},
	}

	_, err := convertMediaPartToGemini(part)
	if err == nil {
		t.Error("Expected error for unreachable URL")
	}

	// MediaLoader now fetches URLs, so we expect connection error
	if err != nil && !strings.Contains(err.Error(), "failed to load image data") {
		t.Errorf("Expected connection error, got: %v", err)
	}
}

func TestGeminiProvider_ConvertMediaPartToGemini_MissingMIMEType(t *testing.T) {
	data := "base64data"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: "", // Missing MIME type
		},
	}

	_, err := convertMediaPartToGemini(part)
	if err == nil {
		t.Error("Expected error for missing MIME type")
	}
}

func TestGeminiProvider_ConvertMediaPartToGemini_MissingDataSource(t *testing.T) {
	part := types.ContentPart{
		Type: types.ContentTypeAudio,
		Media: &types.MediaContent{
			MIMEType: "audio/mp3",
			// No Data, URL, or FilePath
		},
	}

	_, err := convertMediaPartToGemini(part)
	if err == nil {
		t.Error("Expected error for missing data source")
	}
}

func TestGeminiProvider_ConvertPartToGemini_EmptyText(t *testing.T) {
	emptyText := ""
	part := types.ContentPart{
		Type: types.ContentTypeText,
		Text: &emptyText,
	}

	_, err := convertPartToGemini(part)
	if err == nil {
		t.Error("Expected error for empty text")
	}
}

func TestGeminiProvider_ConvertPartToGemini_NilText(t *testing.T) {
	part := types.ContentPart{
		Type: types.ContentTypeText,
		Text: nil,
	}

	_, err := convertPartToGemini(part)
	if err == nil {
		t.Error("Expected error for nil text")
	}
}

func TestGeminiProvider_ConvertPartToGemini_UnsupportedType(t *testing.T) {
	part := types.ContentPart{
		Type: "unsupported-type",
	}

	_, err := convertPartToGemini(part)
	if err == nil {
		t.Error("Expected error for unsupported part type")
	}
}

func TestGeminiProvider_ConvertMediaPartToGemini_MissingMedia(t *testing.T) {
	part := types.ContentPart{
		Type:  types.ContentTypeImage,
		Media: nil,
	}

	_, err := convertMediaPartToGemini(part)
	if err == nil {
		t.Error("Expected error for missing media")
	}
}

func TestGeminiProvider_ConvertMediaPartToGemini_FilePathError(t *testing.T) {
	filePath := "/nonexistent/file.jpg"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			FilePath: &filePath,
			MIMEType: "image/jpeg",
		},
	}

	_, err := convertMediaPartToGemini(part)
	if err == nil {
		t.Error("Expected error for nonexistent file path")
	}
}

func TestGeminiProvider_BuildGeminiRequest(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)

	contents := []geminiContent{
		{
			Role: "user",
			Parts: []geminiPart{
				{Text: "Hello"},
			},
		},
	}

	sysInst := &geminiContent{
		Parts: []geminiPart{
			{Text: "You are helpful"},
		},
	}

	req := provider.buildGeminiRequest(contents, sysInst, 0.7, 0.9, 1000)

	// Verify the request structure
	if len(req.Contents) != 1 {
		t.Error("Contents not set correctly")
	}

	if req.GenerationConfig.Temperature != 0.7 {
		t.Error("Temperature not set correctly")
	}

	if req.GenerationConfig.TopP != 0.9 {
		t.Error("TopP not set correctly")
	}

	if req.GenerationConfig.MaxOutputTokens != 1000 {
		t.Error("MaxOutputTokens not set correctly")
	}

	if req.SystemInstruction == nil {
		t.Error("System instruction should be included")
	}
}

func TestGeminiProvider_ConvertMessagesToGemini_EmptySystem(t *testing.T) {
	messages := []types.Message{
		{
			Role:    "user",
			Content: "Hello",
		},
	}

	contents, sysInst, err := convertMessagesToGemini(messages, "")
	if err != nil {
		t.Fatalf("Failed to convert messages: %v", err)
	}

	if sysInst != nil {
		t.Error("Expected nil system instruction for empty system prompt")
	}

	if len(contents) != 1 {
		t.Errorf("Expected 1 content, got %d", len(contents))
	}
}

func TestGeminiProvider_ConvertMessageToGemini_RoleConversion(t *testing.T) {
	// Test assistant -> model conversion
	msg := types.Message{
		Role:    "assistant",
		Content: "I can help",
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert: %v", err)
	}

	if converted.Role != "model" {
		t.Errorf("Expected role 'model', got '%s'", converted.Role)
	}
}
