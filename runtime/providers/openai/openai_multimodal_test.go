package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIProvider_GetMultimodalCapabilities(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	caps := provider.GetMultimodalCapabilities()

	if !caps.SupportsImages {
		t.Error("Expected OpenAI to support images")
	}

	if caps.SupportsAudio {
		t.Error("Expected OpenAI not to support audio in predict API")
	}

	if caps.SupportsVideo {
		t.Error("Expected OpenAI not to support video in predict API")
	}

	if len(caps.ImageFormats) == 0 {
		t.Error("Expected OpenAI to have supported image formats")
	}

	// Check for common formats
	hasJPEG := false
	hasPNG := false
	for _, format := range caps.ImageFormats {
		if format == types.MIMETypeImageJPEG {
			hasJPEG = true
		}
		if format == types.MIMETypeImagePNG {
			hasPNG = true
		}
	}

	if !hasJPEG || !hasPNG {
		t.Error("Expected OpenAI to support JPEG and PNG formats")
	}

	if caps.MaxImageSizeMB != 20 {
		t.Errorf("Expected MaxImageSizeMB = 20, got %d", caps.MaxImageSizeMB)
	}
}

func TestOpenAIProvider_AudioModelCapabilities(t *testing.T) {
	// Audio models with Chat Completions API should support audio
	provider := NewProviderWithConfig(
		"test-audio", "gpt-4o-audio-preview", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "completions"},
	)

	caps := provider.GetMultimodalCapabilities()

	if !caps.SupportsImages {
		t.Error("Expected audio model to still support images")
	}

	if !caps.SupportsAudio {
		t.Error("Expected audio model with completions API to support audio")
	}

	if caps.SupportsVideo {
		t.Error("Expected audio model not to support video")
	}

	// Check audio formats
	hasWAV := false
	hasMP3 := false
	for _, format := range caps.AudioFormats {
		if format == types.MIMETypeAudioWAV {
			hasWAV = true
		}
		if format == types.MIMETypeAudioMP3 {
			hasMP3 = true
		}
	}

	if !hasWAV || !hasMP3 {
		t.Error("Expected audio model to support WAV and MP3 formats")
	}

	if caps.MaxAudioSizeMB != 25 {
		t.Errorf("Expected MaxAudioSizeMB = 25, got %d", caps.MaxAudioSizeMB)
	}
}

func TestOpenAIProvider_NonAudioModelWithCompletionsAPI(t *testing.T) {
	// Non-audio model with Chat Completions API should NOT support audio
	provider := NewProviderWithConfig(
		"test", "gpt-4o", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "completions"},
	)

	caps := provider.GetMultimodalCapabilities()

	if caps.SupportsAudio {
		t.Error("Expected non-audio model not to support audio even with completions API")
	}
}

func TestOpenAIProvider_SupportsMultimodal(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	if !providers.SupportsMultimodal(provider) {
		t.Error("Expected OpenAI provider to support multimodal")
	}

	if !providers.HasImageSupport(provider) {
		t.Error("Expected OpenAI provider to have image support")
	}

	if providers.HasAudioSupport(provider) {
		t.Error("Expected OpenAI provider not to have audio support")
	}

	if providers.HasVideoSupport(provider) {
		t.Error("Expected OpenAI provider not to have video support")
	}
}

func TestOpenAIProvider_ConvertLegacyMessage(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	legacyMsg := types.Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	converted, err := provider.convertMessageToOpenAI(legacyMsg)
	if err != nil {
		t.Fatalf("Failed to convert legacy message: %v", err)
	}

	if converted.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", converted.Role)
	}

	content, ok := converted.Content.(string)
	if !ok {
		t.Fatalf("Expected content to be string, got %T", converted.Content)
	}

	if content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", content)
	}
}

func TestOpenAIProvider_ConvertTextOnlyMultimodalMessage(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	msg := types.Message{
		Role:  "user",
		Parts: []types.ContentPart{types.NewTextPart("Hello from multimodal!")},
	}

	converted, err := provider.convertMessageToOpenAI(msg)
	if err != nil {
		t.Fatalf("Failed to convert text-only multimodal message: %v", err)
	}

	if converted.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", converted.Role)
	}

	parts, ok := converted.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected content to be []interface{}, got %T", converted.Content)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 content part, got %d", len(parts))
	}

	partMap, ok := parts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected part to be map[string]interface{}, got %T", parts[0])
	}

	if partMap["type"] != "text" {
		t.Errorf("Expected type 'text', got '%v'", partMap["type"])
	}

	if partMap["text"] != "Hello from multimodal!" {
		t.Errorf("Expected text 'Hello from multimodal!', got '%v'", partMap["text"])
	}
}

func TestOpenAIProvider_ConvertImageURLMessage(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	imageURL := "https://example.com/image.jpg"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("What's in this image?"),
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &imageURL,
					MIMEType: types.MIMETypeImageJPEG,
				},
			},
		},
	}

	converted, err := provider.convertMessageToOpenAI(msg)
	if err != nil {
		t.Fatalf("Failed to convert image URL message: %v", err)
	}

	parts, ok := converted.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected content to be []interface{}, got %T", converted.Content)
	}

	if len(parts) != 2 {
		t.Fatalf("Expected 2 content parts, got %d", len(parts))
	}

	// Check text part
	textPart, ok := parts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected text part to be map, got %T", parts[0])
	}
	if textPart["type"] != "text" {
		t.Errorf("Expected type 'text', got '%v'", textPart["type"])
	}

	// Check image part
	imagePart, ok := parts[1].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image part to be map, got %T", parts[1])
	}
	if imagePart["type"] != "image_url" {
		t.Errorf("Expected type 'image_url', got '%v'", imagePart["type"])
	}

	imageURLData, ok := imagePart["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image_url to be map, got %T", imagePart["image_url"])
	}

	if imageURLData["url"] != imageURL {
		t.Errorf("Expected URL '%s', got '%v'", imageURL, imageURLData["url"])
	}
}

func TestOpenAIProvider_ConvertImageBase64Message(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	base64Data := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &base64Data,
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	converted, err := provider.convertMessageToOpenAI(msg)
	if err != nil {
		t.Fatalf("Failed to convert base64 image message: %v", err)
	}

	parts, ok := converted.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected content to be []interface{}, got %T", converted.Content)
	}

	imagePart, ok := parts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image part to be map, got %T", parts[0])
	}

	imageURLData, ok := imagePart["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image_url to be map, got %T", imagePart["image_url"])
	}

	expectedDataURL := "data:image/png;base64," + base64Data
	if imageURLData["url"] != expectedDataURL {
		t.Errorf("Expected data URL '%s', got '%v'", expectedDataURL, imageURLData["url"])
	}
}

func TestOpenAIProvider_ConvertImageWithDetail(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	imageURL := "https://example.com/image.jpg"
	detail := "high"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &imageURL,
					MIMEType: types.MIMETypeImageJPEG,
					Detail:   &detail,
				},
			},
		},
	}

	converted, err := provider.convertMessageToOpenAI(msg)
	if err != nil {
		t.Fatalf("Failed to convert image with detail: %v", err)
	}

	parts, ok := converted.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected content to be []interface{}, got %T", converted.Content)
	}

	imagePart, ok := parts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image part to be map, got %T", parts[0])
	}

	imageURLData, ok := imagePart["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image_url to be map, got %T", imagePart["image_url"])
	}

	if imageURLData["detail"] != detail {
		t.Errorf("Expected detail '%s', got '%v'", detail, imageURLData["detail"])
	}
}

func TestOpenAIProvider_ConvertMultipleImages(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	url1 := "https://example.com/image1.jpg"
	url2 := "https://example.com/image2.jpg"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Compare these images:"),
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &url1,
					MIMEType: types.MIMETypeImageJPEG,
				},
			},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &url2,
					MIMEType: types.MIMETypeImageJPEG,
				},
			},
		},
	}

	converted, err := provider.convertMessageToOpenAI(msg)
	if err != nil {
		t.Fatalf("Failed to convert multiple images: %v", err)
	}

	parts, ok := converted.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected content to be []interface{}, got %T", converted.Content)
	}

	if len(parts) != 3 {
		t.Fatalf("Expected 3 content parts, got %d", len(parts))
	}

	// Verify it's text, image, image
	textPart := parts[0].(map[string]interface{})
	if textPart["type"] != "text" {
		t.Error("Expected first part to be text")
	}

	imagePart1 := parts[1].(map[string]interface{})
	if imagePart1["type"] != "image_url" {
		t.Error("Expected second part to be image_url")
	}

	imagePart2 := parts[2].(map[string]interface{})
	if imagePart2["type"] != "image_url" {
		t.Error("Expected third part to be image_url")
	}
}

func TestOpenAIProvider_ConvertAudioReturnsError(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	audioFile := "/path/to/audio.mp3"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					FilePath: &audioFile,
					MIMEType: types.MIMETypeAudioMP3,
				},
			},
		},
	}

	_, err := provider.convertMessageToOpenAI(msg)
	if err == nil {
		t.Fatal("Expected error when converting audio content, got nil")
	}

	expectedErr := "audio content requires audio model with Chat Completions API"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestOpenAIProvider_ConvertVideoReturnsError(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	videoFile := "/path/to/video.mp4"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeVideo,
				Media: &types.MediaContent{
					FilePath: &videoFile,
					MIMEType: types.MIMETypeVideoMP4,
				},
			},
		},
	}

	_, err := provider.convertMessageToOpenAI(msg)
	if err == nil {
		t.Fatal("Expected error when converting video content, got nil")
	}
}

func TestOpenAIProvider_ConvertEmptyTextPart(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	emptyText := ""
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &emptyText,
			},
		},
	}

	converted, err := provider.convertMessageToOpenAI(msg)
	if err != nil {
		t.Fatalf("Failed to convert empty text part: %v", err)
	}

	// Empty text parts should be skipped
	parts, ok := converted.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected content to be []interface{}, got %T", converted.Content)
	}

	if len(parts) != 0 {
		t.Errorf("Expected 0 content parts (empty text should be skipped), got %d", len(parts))
	}
}

func TestOpenAIProvider_ConvertImageMissingMedia(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type:  types.ContentTypeImage,
				Media: nil, // Missing media content
			},
		},
	}

	_, err := provider.convertMessageToOpenAI(msg)
	if err == nil {
		t.Fatal("Expected error when image part is missing media content, got nil")
	}
}

func TestOpenAIProvider_ConvertImageMissingDataSource(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					MIMEType: types.MIMETypeImageJPEG,
					// No URL, Data, or FilePath
				},
			},
		},
	}

	_, err := provider.convertImagePartToOpenAI(msg.Parts[0])
	if err == nil {
		t.Fatal("Expected error when image has no data source, got nil")
	}
}

func TestExtractContentString_SimpleString(t *testing.T) {
	content := "Hello, world!"
	result := extractContentString(content)

	if result != content {
		t.Errorf("Expected '%s', got '%s'", content, result)
	}
}

func TestExtractContentString_TextParts(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "Hello, ",
		},
		map[string]interface{}{
			"type": "text",
			"text": "world!",
		},
	}

	result := extractContentString(content)
	expected := "Hello, world!"

	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestExtractContentString_MixedParts(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "Check this image: ",
		},
		map[string]interface{}{
			"type": "image_url",
			"url":  "https://example.com/image.jpg",
		},
		map[string]interface{}{
			"type": "text",
			"text": " What do you see?",
		},
	}

	result := extractContentString(content)
	expected := "Check this image:  What do you see?"

	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestExtractContentString_EmptyArray(t *testing.T) {
	content := []interface{}{}
	result := extractContentString(content)

	if result != "" {
		t.Errorf("Expected empty string, got '%s'", result)
	}
}

func TestExtractContentString_InvalidType(t *testing.T) {
	content := 12345
	result := extractContentString(content)

	if result != "" {
		t.Errorf("Expected empty string for invalid type, got '%s'", result)
	}
}

func TestExtractContentString_NilText(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			// Missing text field
		},
	}

	result := extractContentString(content)

	if result != "" {
		t.Errorf("Expected empty string when text field is missing, got '%s'", result)
	}
}

func TestOpenAIProvider_ValidateMultimodalMessage(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	tests := []struct {
		name        string
		message     types.Message
		expectError bool
	}{
		{
			name: "text only message",
			message: types.Message{
				Role:    "user",
				Content: "Hello",
			},
			expectError: false,
		},
		{
			name: "valid image message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewTextPart("What's this?"),
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							URL:      strPtr("https://example.com/image.jpg"),
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "unsupported audio message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							FilePath: strPtr("/path/to/audio.mp3"),
							MIMEType: types.MIMETypeAudioMP3,
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "unsupported video message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							URL:      strPtr("https://example.com/video.mp4"),
							MIMEType: types.MIMETypeVideoMP4,
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "unsupported image format",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							URL:      strPtr("https://example.com/image.bmp"),
							MIMEType: "image/bmp", // Not supported
						},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := providers.ValidateMultimodalMessage(provider, tt.message)
			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestOpenAIProvider_ConvertMessagesToOpenAI(t *testing.T) {
	provider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	req := providers.PredictionRequest{
		System: "You are a helpful assistant.",
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Hello",
			},
			{
				Role:    "assistant",
				Content: "Hi there!",
			},
			{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewTextPart("What's in this image?"),
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							URL:      strPtr("https://example.com/image.jpg"),
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
				},
			},
		},
	}

	messages, err := provider.convertMessagesToOpenAI(req)
	if err != nil {
		t.Fatalf("Failed to convert messages: %v", err)
	}

	// Should have system + 3 user/assistant messages
	if len(messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(messages))
	}

	// Check system message
	if messages[0].Role != "system" {
		t.Errorf("Expected first message role to be 'system', got '%s'", messages[0].Role)
	}
	if messages[0].Content != "You are a helpful assistant." {
		t.Errorf("Unexpected system message content")
	}

	// Check legacy message format
	if messages[1].Role != "user" {
		t.Errorf("Expected second message role to be 'user', got '%s'", messages[1].Role)
	}
	if messages[1].Content != "Hello" {
		t.Errorf("Unexpected user message content")
	}

	// Check multimodal message format
	if messages[3].Role != "user" {
		t.Errorf("Expected fourth message role to be 'user', got '%s'", messages[3].Role)
	}
	parts, ok := messages[3].Content.([]interface{})
	if !ok {
		t.Fatalf("Expected multimodal content to be []interface{}, got %T", messages[3].Content)
	}
	if len(parts) != 2 {
		t.Errorf("Expected 2 content parts in multimodal message, got %d", len(parts))
	}
}

func TestOpenAIMessage_JSONMarshaling(t *testing.T) {
	tests := []struct {
		name    string
		message openAIMessage
	}{
		{
			name: "string content",
			message: openAIMessage{
				Role:    "user",
				Content: "Hello",
			},
		},
		{
			name: "multimodal content",
			message: openAIMessage{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Hello",
					},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "https://example.com/image.jpg",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.message)
			if err != nil {
				t.Fatalf("Failed to marshal message: %v", err)
			}

			// Unmarshal
			var unmarshaled openAIMessage
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal message: %v", err)
			}

			// Verify role is preserved
			if unmarshaled.Role != tt.message.Role {
				t.Errorf("Role mismatch: expected '%s', got '%s'", tt.message.Role, unmarshaled.Role)
			}
		})
	}
}

// TestPredictMultimodal_Integration tests the full PredictMultimodal flow with HTTP mocking
func TestPredictMultimodal_Integration(t *testing.T) {
	tests := []struct {
		name           string
		messages       []types.Message
		serverResponse openAIResponse
		serverStatus   int
		wantErr        bool
		errContains    string
		validateReq    func(t *testing.T, req map[string]interface{})
	}{
		{
			name: "Successful multimodal request with image",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("What's in this image?"),
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								URL:      strPtr("https://example.com/test.jpg"),
								MIMEType: types.MIMETypeImageJPEG,
							},
						},
					},
				},
			},
			serverResponse: openAIResponse{
				ID:      "predictcmpl-123",
				Object:  "predict.completion",
				Created: time.Now().Unix(),
				Model:   "gpt-4o",
				Choices: []openAIChoice{
					{
						Index: 0,
						Message: openAIMessage{
							Role:    "assistant",
							Content: "I see a beautiful image.",
						},
						FinishReason: "stop",
					},
				},
				Usage: openAIUsage{
					PromptTokens:     150,
					CompletionTokens: 50,
					TotalTokens:      200,
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
			validateReq: func(t *testing.T, req map[string]interface{}) {
				messages, ok := req["messages"].([]interface{})
				require.True(t, ok, "messages should be array")
				require.Len(t, messages, 1)

				msg := messages[0].(map[string]interface{})
				content, ok := msg["content"].([]interface{})
				require.True(t, ok, "content should be array for multimodal")
				require.Len(t, content, 2, "should have text and image parts")
			},
		},
		{
			name: "API error response",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("Test"),
					},
				},
			},
			serverResponse: openAIResponse{
				Error: &openAIError{
					Message: "Invalid request",
					Type:    "invalid_request_error",
					Code:    "invalid_prompt",
				},
			},
			serverStatus: http.StatusBadRequest,
			wantErr:      true,
			errContains:  "400",
		},
		{
			name: "Empty response choices",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("Test"),
					},
				},
			},
			serverResponse: openAIResponse{
				ID:      "predictcmpl-123",
				Choices: []openAIChoice{},
			},
			serverStatus: http.StatusOK,
			wantErr:      true,
			errContains:  "no choices in response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			var capturedRequest map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Capture the request
				if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
					t.Fatalf("Failed to decode request: %v", err)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.serverStatus)
				json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			// Create provider
			provider := NewProvider(
				"test",
				"gpt-4o",
				server.URL,
				providers.ProviderDefaults{
					Temperature: 0.7,
					TopP:        0.9,
					MaxTokens:   1000,
				},
				false,
			)

			// Create request
			req := providers.PredictionRequest{
				Messages:    tt.messages,
				Temperature: 0.8,
				MaxTokens:   500,
			}

			// Execute
			resp, err := provider.PredictMultimodal(context.Background(), req)

			// Validate error
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			// Validate success
			require.NoError(t, err)
			assert.NotEmpty(t, resp.Content)
			assert.NotNil(t, resp.CostInfo)
			assert.Greater(t, resp.Latency, time.Duration(0))

			// Validate request if callback provided
			if tt.validateReq != nil && capturedRequest != nil {
				tt.validateReq(t, capturedRequest)
			}
		})
	}
}

// TestPredictMultimodalStream_Integration tests the streaming multimodal flow
func TestPredictMultimodalStream_Integration(t *testing.T) {
	tests := []struct {
		name         string
		messages     []types.Message
		serverChunks []string
		wantErr      bool
		errContains  string
	}{
		{
			name: "Successful streaming with image",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("Describe this"),
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								URL:      strPtr("https://example.com/image.png"),
								MIMEType: types.MIMETypeImagePNG,
							},
						},
					},
				},
			},
			serverChunks: []string{
				`data: {"choices":[{"delta":{"content":"This"},"finish_reason":null}]}`,
				`data: {"choices":[{"delta":{"content":" is"},"finish_reason":null}]}`,
				`data: {"choices":[{"delta":{"content":" nice"},"finish_reason":null}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":30,"total_tokens":130}}`,
				`data: [DONE]`,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)

				flusher, ok := w.(http.Flusher)
				require.True(t, ok, "ResponseWriter should support flushing")

				for _, chunk := range tt.serverChunks {
					w.Write([]byte(chunk + "\n\n"))
					flusher.Flush()
				}
			}))
			defer server.Close()

			// Create provider
			provider := NewProvider(
				"test",
				"gpt-4o",
				server.URL,
				providers.ProviderDefaults{Temperature: 0.7},
				false,
			)

			// Create request
			req := providers.PredictionRequest{
				Messages: tt.messages,
			}

			// Execute
			streamChan, err := provider.PredictMultimodalStream(context.Background(), req)

			// Validate error
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			// Validate success
			require.NoError(t, err)
			require.NotNil(t, streamChan)

			// Collect chunks
			var chunks []providers.StreamChunk
			for chunk := range streamChan {
				chunks = append(chunks, chunk)
			}

			// Should have received multiple chunks
			assert.NotEmpty(t, chunks)

			// Last chunk should have finish reason
			lastChunk := chunks[len(chunks)-1]
			assert.NotNil(t, lastChunk.FinishReason)
		})
	}
}

// TestPredictWithMessages_ErrorHandling tests error paths in the internal predictWithMessages helper
func TestPredictWithMessages_ErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		messages       []types.Message
		serverStatus   int
		serverResponse interface{}
		wantErr        bool
		errContains    string
	}{
		{
			name: "Malformed JSON response",
			messages: []types.Message{
				{Role: "user", Content: "test"},
			},
			serverStatus:   http.StatusOK,
			serverResponse: "invalid json{",
			wantErr:        true,
			errContains:    "failed to unmarshal response",
		},
		{
			name: "Network timeout simulation",
			messages: []types.Message{
				{Role: "user", Content: "test"},
			},
			serverStatus: http.StatusRequestTimeout,
			serverResponse: map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Request timeout",
					"type":    "timeout",
				},
			},
			wantErr:     true,
			errContains: "408",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				if str, ok := tt.serverResponse.(string); ok {
					w.Write([]byte(str))
				} else {
					json.NewEncoder(w).Encode(tt.serverResponse)
				}
			}))
			defer server.Close()

			provider := NewProvider(
				"test",
				"gpt-4o",
				server.URL,
				providers.ProviderDefaults{Temperature: 0.7},
				false,
			)

			req := providers.PredictionRequest{
				Messages: tt.messages,
			}

			_, err := provider.PredictMultimodal(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestPredictStreamWithMessages_Cancellation tests context cancellation during streaming
func TestPredictStreamWithMessages_Cancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)

		// Send a few chunks then simulate delay
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"Start"}}]}` + "\n\n"))
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`data: {"choices":[{"delta":{"content":" middle"}}]}` + "\n\n"))
		flusher.Flush()

		time.Sleep(5 * time.Second) // This should be cancelled
	}))
	defer server.Close()

	provider := NewProvider("test", "gpt-4o", server.URL, providers.ProviderDefaults{}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	streamChan, err := provider.PredictMultimodalStream(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var lastChunk providers.StreamChunk
	for chunk := range streamChan {
		lastChunk = chunk
	}

	// Should have error or cancelled finish reason
	if lastChunk.Error != nil {
		assert.Contains(t, lastChunk.Error.Error(), "context")
	} else {
		assert.NotNil(t, lastChunk.FinishReason)
		assert.Equal(t, "cancelled", *lastChunk.FinishReason)
	}
}

// TestPredictStreamWithMessages_MalformedChunks tests handling of malformed SSE chunks
func TestPredictStreamWithMessages_MalformedChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)

		// Send good chunk
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"Good"}}]}` + "\n\n"))
		flusher.Flush()

		// Send malformed chunk (should be skipped)
		w.Write([]byte(`data: {invalid json` + "\n\n"))
		flusher.Flush()

		// Send done
		w.Write([]byte(`data: [DONE]` + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewProvider("test", "gpt-4o", server.URL, providers.ProviderDefaults{}, false)

	streamChan, err := provider.PredictMultimodalStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var chunks []providers.StreamChunk
	for chunk := range streamChan {
		chunks = append(chunks, chunk)
	}

	// Should still complete successfully (malformed chunk skipped)
	assert.NotEmpty(t, chunks)
	lastChunk := chunks[len(chunks)-1]
	assert.NotNil(t, lastChunk.FinishReason)
}

// Helper function
func strPtr(s string) *string {
	return &s
}

// =============================================================================
// Audio Model Tests
// =============================================================================

func TestIsAudioModel(t *testing.T) {
	tests := []struct {
		model    string
		expected bool
	}{
		{"gpt-4o-audio-preview", true},
		{"gpt-4o-mini-audio-preview", true},
		{"gpt-4o", false},
		{"gpt-4o-mini", false},
		{"gpt-4-turbo", false},
		{"gpt-3.5-turbo", false},
		{"o1", false},
		{"o1-mini", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := isAudioModel(tt.model)
			if result != tt.expected {
				t.Errorf("isAudioModel(%q) = %v, expected %v", tt.model, result, tt.expected)
			}
		})
	}
}

func TestGetAudioFormat(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{types.MIMETypeAudioWAV, "wav"},
		{"audio/x-wav", "wav"},
		{types.MIMETypeAudioMP3, "mp3"},
		{"audio/mpeg", "mp3"},
		{types.MIMETypeAudioOgg, ""},     // Not supported
		{"audio/flac", ""},               // Not supported
		{"audio/aac", ""},                // Not supported
		{"video/mp4", ""},                // Not audio
		{"", ""},                         // Empty
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := getAudioFormat(tt.mimeType)
			if result != tt.expected {
				t.Errorf("getAudioFormat(%q) = %q, expected %q", tt.mimeType, result, tt.expected)
			}
		})
	}
}

func TestRequestContainsAudio(t *testing.T) {
	tests := []struct {
		name     string
		req      providers.PredictionRequest
		expected bool
	}{
		{
			name: "no messages",
			req: providers.PredictionRequest{
				Messages: []types.Message{},
			},
			expected: false,
		},
		{
			name: "text only message",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			expected: false,
		},
		{
			name: "image only message",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							{
								Type: types.ContentTypeImage,
								Media: &types.MediaContent{
									URL:      strPtr("https://example.com/image.jpg"),
									MIMEType: types.MIMETypeImageJPEG,
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "audio message",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							{
								Type: types.ContentTypeAudio,
								Media: &types.MediaContent{
									Data:     strPtr("base64audiodata"),
									MIMEType: types.MIMETypeAudioWAV,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "mixed content with audio",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							types.NewTextPart("Listen to this:"),
							{
								Type: types.ContentTypeAudio,
								Media: &types.MediaContent{
									Data:     strPtr("base64audiodata"),
									MIMEType: types.MIMETypeAudioMP3,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "audio in second message",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
					{
						Role: "user",
						Parts: []types.ContentPart{
							{
								Type: types.ContentTypeAudio,
								Media: &types.MediaContent{
									Data:     strPtr("base64audiodata"),
									MIMEType: types.MIMETypeAudioWAV,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := requestContainsAudio(&tt.req)
			if result != tt.expected {
				t.Errorf("requestContainsAudio() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestOpenAIProvider_ConvertAudioPartToOpenAI(t *testing.T) {
	provider := NewProviderWithConfig(
		"test-audio", "gpt-4o-audio-preview", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "completions"},
	)

	t.Run("valid WAV audio with base64 data", func(t *testing.T) {
		base64Data := "UklGRhQAAABXQVZFZm10IBAAAAABAAEAIlYAAESsAAACABAAZGF0YQAAAAA="
		part := types.ContentPart{
			Type: types.ContentTypeAudio,
			Media: &types.MediaContent{
				Data:     &base64Data,
				MIMEType: types.MIMETypeAudioWAV,
			},
		}

		result, err := provider.convertAudioPartToOpenAI(part)
		require.NoError(t, err)

		assert.Equal(t, "input_audio", result["type"])
		inputAudio, ok := result["input_audio"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, base64Data, inputAudio["data"])
		assert.Equal(t, "wav", inputAudio["format"])
	})

	t.Run("valid MP3 audio", func(t *testing.T) {
		base64Data := "//uQxAAAAAANIAAAAAExBTUUzLjEwMFVVVVVV"
		part := types.ContentPart{
			Type: types.ContentTypeAudio,
			Media: &types.MediaContent{
				Data:     &base64Data,
				MIMEType: types.MIMETypeAudioMP3,
			},
		}

		result, err := provider.convertAudioPartToOpenAI(part)
		require.NoError(t, err)

		inputAudio := result["input_audio"].(map[string]interface{})
		assert.Equal(t, "mp3", inputAudio["format"])
	})

	t.Run("unsupported audio format", func(t *testing.T) {
		base64Data := "base64flacdata"
		part := types.ContentPart{
			Type: types.ContentTypeAudio,
			Media: &types.MediaContent{
				Data:     &base64Data,
				MIMEType: "audio/flac",
			},
		}

		_, err := provider.convertAudioPartToOpenAI(part)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported audio format")
	})

	t.Run("nil media", func(t *testing.T) {
		part := types.ContentPart{
			Type:  types.ContentTypeAudio,
			Media: nil,
		}

		_, err := provider.convertAudioPartToOpenAI(part)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing media content")
	})
}

func TestOpenAIProvider_AudioModelConvertMessage(t *testing.T) {
	provider := NewProviderWithConfig(
		"test-audio", "gpt-4o-audio-preview", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "completions"},
	)

	t.Run("audio message with text", func(t *testing.T) {
		base64Audio := "UklGRhQAAABXQVZFZm10IBAAAAABAAEAIlYAAESsAAACABAAZGF0YQAAAAA="
		msg := types.Message{
			Role: "user",
			Parts: []types.ContentPart{
				types.NewTextPart("Transcribe this audio:"),
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						Data:     &base64Audio,
						MIMEType: types.MIMETypeAudioWAV,
					},
				},
			},
		}

		converted, err := provider.convertMessageToOpenAI(msg)
		require.NoError(t, err)

		parts, ok := converted.Content.([]interface{})
		require.True(t, ok)
		require.Len(t, parts, 2)

		// Check text part
		textPart := parts[0].(map[string]interface{})
		assert.Equal(t, "text", textPart["type"])

		// Check audio part
		audioPart := parts[1].(map[string]interface{})
		assert.Equal(t, "input_audio", audioPart["type"])
		inputAudio := audioPart["input_audio"].(map[string]interface{})
		assert.Equal(t, "wav", inputAudio["format"])
	})
}

func TestOpenAIProvider_AudioModelWithResponsesAPI(t *testing.T) {
	// Audio model with Responses API should NOT support audio
	provider := NewProviderWithConfig(
		"test-audio", "gpt-4o-audio-preview", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "responses"}, // Not completions
	)

	caps := provider.GetMultimodalCapabilities()

	if caps.SupportsAudio {
		t.Error("Expected audio model with Responses API not to support audio")
	}
}

func TestOpenAIProvider_AudioModelMiniVariant(t *testing.T) {
	// Test the mini variant of audio model
	provider := NewProviderWithConfig(
		"test-audio", "gpt-4o-mini-audio-preview", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "completions"},
	)

	caps := provider.GetMultimodalCapabilities()

	if !caps.SupportsAudio {
		t.Error("Expected gpt-4o-mini-audio-preview to support audio")
	}
	if !caps.SupportsImages {
		t.Error("Expected gpt-4o-mini-audio-preview to still support images")
	}
}

func TestOpenAIProvider_AudioNotSupportedOnNonAudioModel(t *testing.T) {
	// Standard model should reject audio even with completions API
	provider := NewProviderWithConfig(
		"test", "gpt-4o", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "completions"},
	)

	base64Audio := "audiodata"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &base64Audio,
					MIMEType: types.MIMETypeAudioWAV,
				},
			},
		},
	}

	_, err := provider.convertMessageToOpenAI(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audio content requires audio model")
}

func TestOpenAIProvider_APIMode_Configuration(t *testing.T) {
	tests := []struct {
		name            string
		additionalConfig map[string]any
		expectedAPIMode string
	}{
		{
			name:            "default is responses",
			additionalConfig: nil,
			expectedAPIMode: "responses",
		},
		{
			name:            "explicit completions",
			additionalConfig: map[string]any{"api_mode": "completions"},
			expectedAPIMode: "completions",
		},
		{
			name:            "explicit responses",
			additionalConfig: map[string]any{"api_mode": "responses"},
			expectedAPIMode: "responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProviderWithConfig(
				"test", "gpt-4o", "https://api.openai.com/v1",
				providers.ProviderDefaults{}, false,
				tt.additionalConfig,
			)

			// Check the internal API mode
			actualMode := string(provider.apiMode)
			if actualMode != tt.expectedAPIMode {
				t.Errorf("expected API mode %q, got %q", tt.expectedAPIMode, actualMode)
			}
		})
	}
}
