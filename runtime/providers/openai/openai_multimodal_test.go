package openai

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestOpenAIProvider_GetMultimodalCapabilities(t *testing.T) {
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	caps := provider.GetMultimodalCapabilities()

	if !caps.SupportsImages {
		t.Error("Expected OpenAI to support images")
	}

	if caps.SupportsAudio {
		t.Error("Expected OpenAI not to support audio in chat API")
	}

	if caps.SupportsVideo {
		t.Error("Expected OpenAI not to support video in chat API")
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

func TestOpenAIProvider_SupportsMultimodal(t *testing.T) {
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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

	expectedErr := "audio and video content not supported by OpenAI chat API"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestOpenAIProvider_ConvertVideoReturnsError(t *testing.T) {
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	req := providers.ChatRequest{
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

// Helper function
func strPtr(s string) *string {
	return &s
}
