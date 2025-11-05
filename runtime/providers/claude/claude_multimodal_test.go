package claude

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestClaudeProvider_GetMultimodalCapabilities(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	caps := provider.GetMultimodalCapabilities()

	if !caps.SupportsImages {
		t.Error("Expected Claude to support images")
	}

	if caps.SupportsAudio {
		t.Error("Expected Claude to not support audio")
	}

	if caps.SupportsVideo {
		t.Error("Expected Claude to not support video")
	}

	if len(caps.ImageFormats) == 0 {
		t.Error("Expected Claude to have supported image formats")
	}

	if caps.MaxImageSizeMB != 5 {
		t.Errorf("Expected max image size of 5MB, got %d", caps.MaxImageSizeMB)
	}
}

func TestClaudeProvider_SupportsMultimodal(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	if !providers.SupportsMultimodal(provider) {
		t.Error("Expected Claude to support multimodal")
	}
}

func TestClaudeProvider_ConvertLegacyMessage(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	msg := types.Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	claudeMsg, err := provider.convertMessageToClaudeMultimodal(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if claudeMsg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", claudeMsg.Role)
	}

	if len(claudeMsg.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(claudeMsg.Content))
	}
}

func TestClaudeProvider_ConvertTextOnlyMultimodalMessage(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("What's the weather?"),
			types.NewTextPart("I need to know for tomorrow."),
		},
	}

	claudeMsg, err := provider.convertMessageToClaudeMultimodal(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if claudeMsg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", claudeMsg.Role)
	}

	if len(claudeMsg.Content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(claudeMsg.Content))
	}
}

func TestClaudeProvider_ConvertImageBase64Message(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	data := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("What's in this image?"),
			types.NewImagePartFromData(data, types.MIMETypeImagePNG, nil),
		},
	}

	claudeMsg, err := provider.convertMessageToClaudeMultimodal(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if len(claudeMsg.Content) != 2 {
		t.Fatalf("Expected 2 content blocks (text + image), got %d", len(claudeMsg.Content))
	}

	// Parse the content to check image block
	// The content is stored as claudeContentBlock (from base provider)
	// but we need to verify the image was included
}

func TestClaudeProvider_ConvertImageURLMessage(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	url := "https://example.com/image.jpg"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Describe this image"),
			types.NewImagePartFromURL(url, nil),
		},
	}

	claudeMsg, err := provider.convertMessageToClaudeMultimodal(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if len(claudeMsg.Content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(claudeMsg.Content))
	}
}

func TestClaudeProvider_ConvertMultipleImages(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	data1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	data2 := "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFElEQVR42mNk+M9QzzCAARMOBgYA0wEIVlDAywQAAAAASUVORK5CYII="

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Compare these two images"),
			types.NewImagePartFromData(data1, types.MIMETypeImagePNG, nil),
			types.NewImagePartFromData(data2, types.MIMETypeImagePNG, nil),
		},
	}

	claudeMsg, err := provider.convertMessageToClaudeMultimodal(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if len(claudeMsg.Content) != 3 {
		t.Fatalf("Expected 3 content blocks (text + 2 images), got %d", len(claudeMsg.Content))
	}
}

func TestClaudeProvider_ConvertAudioReturnsError(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Transcribe this audio"),
			types.NewAudioPartFromData("base64audiodata", types.MIMETypeAudioMP3),
		},
	}

	_, err := provider.convertMessageToClaudeMultimodal(msg)
	if err == nil {
		t.Error("Expected error for audio content, got nil")
	}

	if err != nil && err.Error() != "claude does not support audio content" {
		t.Errorf("Expected audio not supported error, got: %v", err)
	}
}

func TestClaudeProvider_ConvertVideoReturnsError(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewVideoPartFromData("base64videodata", types.MIMETypeVideoMP4),
		},
	}

	_, err := provider.convertMessageToClaudeMultimodal(msg)
	if err == nil {
		t.Error("Expected error for video content, got nil")
	}

	if err != nil && err.Error() != "claude does not support video content" {
		t.Errorf("Expected video not supported error, got: %v", err)
	}
}

func TestClaudeProvider_ConvertEmptyTextPart(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	emptyText := ""
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &emptyText},
			types.NewTextPart("Non-empty text"),
		},
	}

	claudeMsg, err := provider.convertMessageToClaudeMultimodal(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	// Empty text parts should be skipped
	if len(claudeMsg.Content) != 1 {
		t.Errorf("Expected 1 content block (empty text skipped), got %d", len(claudeMsg.Content))
	}
}

func TestClaudeProvider_ConvertImageMissingMedia(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeImage, Media: nil},
		},
	}

	_, err := provider.convertMessageToClaudeMultimodal(msg)
	if err == nil {
		t.Error("Expected error for image without media, got nil")
	}
}

func TestClaudeProvider_ConvertImageMissingDataSource(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					MIMEType: types.MIMETypeImageJPEG,
					// No Data, URL, or FilePath set
				},
			},
		},
	}

	_, err := provider.convertMessageToClaudeMultimodal(msg)
	if err == nil {
		t.Error("Expected error for image without data source, got nil")
	}

	if err != nil && err.Error() != "no data source specified for image" {
		t.Errorf("Expected 'no data source' error, got: %v", err)
	}
}

func TestClaudeProvider_ValidateMultimodalMessage(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	tests := []struct {
		name    string
		message types.Message
		wantErr bool
	}{
		{
			name: "text only message",
			message: types.Message{
				Role:    "user",
				Content: "Hello",
			},
			wantErr: false,
		},
		{
			name: "valid image message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewTextPart("What's this?"),
					types.NewImagePartFromData("base64data", types.MIMETypeImageJPEG, nil),
				},
			},
			wantErr: false,
		},
		{
			name: "unsupported audio message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewAudioPartFromData("audiodata", types.MIMETypeAudioMP3),
				},
			},
			wantErr: true,
		},
		{
			name: "unsupported video message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewVideoPartFromData("videodata", types.MIMETypeVideoMP4),
				},
			},
			wantErr: true,
		},
		{
			name: "unsupported image format",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewImagePartFromData("data", "image/bmp", nil),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := providers.ValidateMultimodalMessage(provider, tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("providers.ValidateMultimodalMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClaudeProvider_ConvertMessagesToClaudeMultimodal(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	messages := []types.Message{
		{
			Role:    "system",
			Content: "You are a helpful assistant.",
		},
		{
			Role:    "user",
			Content: "Hello!",
		},
		{
			Role: "assistant",
			Parts: []types.ContentPart{
				types.NewTextPart("Hi! How can I help you?"),
			},
		},
		{
			Role: "user",
			Parts: []types.ContentPart{
				types.NewTextPart("What's in this image?"),
				types.NewImagePartFromData("base64data", types.MIMETypeImageJPEG, nil),
			},
		},
	}

	claudeMessages, systemBlocks, err := provider.convertMessagesToClaudeMultimodal(messages)
	if err != nil {
		t.Fatalf("Failed to convert messages: %v", err)
	}

	// System message should be separated
	if len(systemBlocks) != 1 {
		t.Errorf("Expected 1 system block, got %d", len(systemBlocks))
	}

	// Should have 3 non-system messages
	if len(claudeMessages) != 3 {
		t.Errorf("Expected 3 claude messages (excluding system), got %d", len(claudeMessages))
	}

	// Last message should have 2 content parts (text + image)
	if len(claudeMessages[2].Content) != 2 {
		t.Errorf("Expected last message to have 2 content parts, got %d", len(claudeMessages[2].Content))
	}
}

func TestClaudeProvider_ImageFormatsSupported(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	supportedFormats := []string{
		types.MIMETypeImageJPEG,
		types.MIMETypeImagePNG,
		types.MIMETypeImageGIF,
		types.MIMETypeImageWebP,
	}

	for _, format := range supportedFormats {
		if !providers.IsFormatSupported(provider, types.ContentTypeImage, format) {
			t.Errorf("Expected Claude to support %s", format)
		}
	}

	// Test unsupported format
	if providers.IsFormatSupported(provider, types.ContentTypeImage, "image/bmp") {
		t.Error("Expected Claude to not support image/bmp")
	}
}

func TestClaudeProvider_MixedMultimodal(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Here's an analysis request:"),
			types.NewImagePartFromURL("https://example.com/chart.png", nil),
			types.NewTextPart("What trends do you see?"),
			types.NewImagePartFromData("base64data", types.MIMETypeImageJPEG, nil),
			types.NewTextPart("How do they compare?"),
		},
	}

	claudeMsg, err := provider.convertMessageToClaudeMultimodal(msg)
	if err != nil {
		t.Fatalf("Failed to convert mixed multimodal message: %v", err)
	}

	// Should have 5 content parts (3 text + 2 images)
	if len(claudeMsg.Content) != 5 {
		t.Errorf("Expected 5 content parts, got %d", len(claudeMsg.Content))
	}
}

func TestClaudeProvider_ParseClaudeResponse(t *testing.T) {
	// Test valid response
	validJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-3-5-sonnet-20241022",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5
		}
	}`

	resp, err := parseClaudeResponse([]byte(validJSON))
	if err != nil {
		t.Fatalf("Failed to parse valid response: %v", err)
	}

	if len(resp.Content) == 0 {
		t.Error("Expected content in response")
	}

	// Test error response
	errorJSON := `{
		"type": "error",
		"error": {
			"type": "invalid_request_error",
			"message": "Invalid API key"
		}
	}`

	_, err = parseClaudeResponse([]byte(errorJSON))
	if err == nil {
		t.Error("Expected error for error response, got nil")
	}

	// Test empty content response
	emptyJSON := `{
		"id": "msg_456",
		"type": "message",
		"role": "assistant",
		"content": [],
		"model": "claude-3-5-sonnet-20241022",
		"usage": {"input_tokens": 10, "output_tokens": 0}
	}`

	_, err = parseClaudeResponse([]byte(emptyJSON))
	if err == nil {
		t.Error("Expected error for empty content, got nil")
	}
}
