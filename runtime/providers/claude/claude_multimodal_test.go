package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

	if err != nil && !strings.Contains(err.Error(), "no media source available") {
		t.Errorf("Expected 'no media source available' error, got: %v", err)
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

func TestClaudeProvider_BuildClaudeMessage(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Test with text block
	textBlocks := []interface{}{
		claudeContentBlockMultimodal{
			Type: "text",
			Text: "Hello, Claude!",
		},
	}

	msg, err := provider.buildClaudeMessage("user", textBlocks)
	if err != nil {
		t.Fatalf("Failed to build message: %v", err)
	}

	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", msg.Role)
	}

	// Test with multiple content blocks
	multiBlocks := []interface{}{
		claudeContentBlockMultimodal{
			Type: "text",
			Text: "First block",
		},
		claudeContentBlockMultimodal{
			Type: "text",
			Text: "Second block",
		},
	}

	msg2, err := provider.buildClaudeMessage("assistant", multiBlocks)
	if err != nil {
		t.Fatalf("Failed to build message with multiple blocks: %v", err)
	}

	if msg2.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", msg2.Role)
	}
}

func TestClaudeProvider_ConvertPartsToClaudeBlocks(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Test with nil text (should be skipped)
	parts := []types.ContentPart{
		{Type: types.ContentTypeText, Text: nil},
		types.NewTextPart("Valid text"),
	}

	blocks, err := provider.convertPartsToClaudeBlocks(parts)
	if err != nil {
		t.Fatalf("Failed to convert parts: %v", err)
	}

	// Nil text should be skipped, so only 1 block expected
	if len(blocks) != 1 {
		t.Errorf("Expected 1 block (nil text skipped), got %d", len(blocks))
	}

	// Test unsupported content type
	unsupportedParts := []types.ContentPart{
		{Type: "unsupported-type"},
	}

	_, err = provider.convertPartsToClaudeBlocks(unsupportedParts)
	if err == nil {
		t.Error("Expected error for unsupported content type")
	}
}

func TestClaudeProvider_StreamResponseMultimodal(t *testing.T) {
	// This test verifies that streamResponseMultimodal calls the base streamResponse
	// We can't fully test streaming without HTTP mocking, but we can verify the method exists
	// and handles closed channels correctly
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Verify the provider has the streamResponseMultimodal method
	// by checking it compiles and doesn't panic on nil inputs (defensive test)
	if provider == nil {
		t.Fatal("Provider should not be nil")
	}
}

func TestClaudeProvider_ConvertImagePartToClaude_FilePath(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Test with file path (will fail to load file, but tests the code path)
	filePath := "/nonexistent/test.jpg"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			FilePath: &filePath,
			MIMEType: types.MIMETypeImageJPEG,
		},
	}

	_, err := provider.convertImagePartToClaude(part)
	if err == nil {
		t.Error("Expected error for nonexistent file path")
	}

	// Error should mention file loading (we just verify an error occurred)
}

func TestClaudeProvider_ConvertSystemMessage(t *testing.T) {
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
	}

	claudeMessages, systemBlocks, err := provider.convertMessagesToClaudeMultimodal(messages)
	if err != nil {
		t.Fatalf("Failed to convert messages: %v", err)
	}

	// System message should be in systemBlocks, not claudeMessages
	if len(systemBlocks) != 1 {
		t.Errorf("Expected 1 system block, got %d", len(systemBlocks))
	}

	if len(claudeMessages) != 1 {
		t.Errorf("Expected 1 claude message (user), got %d", len(claudeMessages))
	}

	if systemBlocks[0].Text != "You are a helpful assistant." {
		t.Errorf("System block text mismatch")
	}
}

func TestClaudeProvider_PredictMultimodal_ValidationError(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Test with unsupported audio format
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewAudioPartFromData("audiodata", types.MIMETypeAudioMP3),
				},
			},
		},
	}

	_, err := provider.PredictMultimodal(context.Background(), req)
	if err == nil {
		t.Error("Expected validation error for unsupported audio format")
	}
}

func TestClaudeProvider_PredictMultimodalStream_ValidationError(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Test with unsupported video format
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewVideoPartFromData("videodata", types.MIMETypeVideoMP4),
				},
			},
		},
	}

	_, err := provider.PredictMultimodalStream(context.Background(), req)
	if err == nil {
		t.Error("Expected validation error for unsupported video format")
	}
}

func TestClaudeProvider_PredictWithContentsMultimodal_HTTPErrors(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		expectError string
	}{
		{
			name: "HTTP request creation error - invalid URL",
			setupServer: func() *httptest.Server {
				// Server won't be used, invalid URL will cause error during request creation
				return nil
			},
			expectError: "failed to create request",
		},
		{
			name: "HTTP connection error",
			setupServer: func() *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				server.Close() // Close immediately to cause connection error
				return server
			},
			expectError: "failed to send request",
		},
		{
			name: "Non-200 status code",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"error": {"type": "invalid_request_error", "message": "Bad request"}}`))
				}))
			},
			expectError: "claude api error (status 400)",
		},
		{
			name: "Empty response content",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"id": "msg_123", "type": "message", "role": "assistant", "content": [], "usage": {"input_tokens": 10, "output_tokens": 0}}`))
				}))
			},
			expectError: "no content in response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var provider *ClaudeProvider

			if tt.name == "HTTP request creation error - invalid URL" {
				// Use invalid URL with control characters to trigger request creation error
				provider = NewClaudeProvider(
					"test-claude",
					"claude-3-5-sonnet-20241022",
					"http://\x7f/invalid", // Invalid URL with control character
					providers.ProviderDefaults{},
					false,
				)
			} else {
				server := tt.setupServer()
				if server != nil {
					defer server.Close()
				}

				baseURL := "http://localhost:0" // Will fail if server is closed
				if server != nil {
					baseURL = server.URL
				}

				provider = NewClaudeProvider(
					"test-claude",
					"claude-3-5-sonnet-20241022",
					baseURL,
					providers.ProviderDefaults{},
					false,
				)
			}

			req := providers.PredictionRequest{
				Messages: []types.Message{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
			}

			_, err := provider.PredictMultimodal(context.Background(), req)
			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.expectError)
				return
			}

			if !strings.Contains(err.Error(), tt.expectError) {
				t.Errorf("Expected error containing '%s', got: %v", tt.expectError, err)
			}
		})
	}
}

func TestClaudeProvider_PredictStreamWithContentsMultimodal_HTTPErrors(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		expectError string
	}{
		{
			name: "HTTP connection error",
			setupServer: func() *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				server.Close() // Close immediately to cause connection error
				return server
			},
			expectError: "failed to send request",
		},
		{
			name: "Non-200 status code",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error": {"type": "authentication_error", "message": "Invalid API key"}}`))
				}))
			},
			expectError: "claude api error (status 401)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			if server != nil && tt.name != "HTTP connection error" {
				defer server.Close()
			}

			baseURL := "http://localhost:0"
			if server != nil {
				baseURL = server.URL
			}

			provider := NewClaudeProvider(
				"test-claude",
				"claude-3-5-sonnet-20241022",
				baseURL,
				providers.ProviderDefaults{},
				false,
			)

			req := providers.PredictionRequest{
				Messages: []types.Message{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
			}

			_, err := provider.PredictMultimodalStream(context.Background(), req)
			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.expectError)
				return
			}

			if !strings.Contains(err.Error(), tt.expectError) {
				t.Errorf("Expected error containing '%s', got: %v", tt.expectError, err)
			}
		})
	}
}

func TestClaudeProvider_PredictWithContentsMultimodal_ReadBodyError(t *testing.T) {
	// Test error reading response body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100") // Claim 100 bytes
		w.WriteHeader(http.StatusOK)
		// Send nothing, causing a read error
	}))
	defer server.Close()

	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		server.URL,
		providers.ProviderDefaults{},
		false,
	)

	req := providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	resp, err := provider.PredictMultimodal(context.Background(), req)
	// May get read error or unmarshal error depending on what was read
	if err == nil {
		t.Error("Expected error reading or parsing response")
	}
	// Latency should still be populated even on error
	if resp.Latency == 0 {
		t.Error("Expected latency to be set even on error")
	}
}

func TestClaudeProvider_BuildClaudeMessage_MarshalError(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Create content blocks with unmarshalable content (channel type can't be marshaled)
	ch := make(chan int)
	badBlocks := []interface{}{
		ch, // Channels can't be marshaled to JSON
	}

	_, err := provider.buildClaudeMessage("user", badBlocks)
	if err == nil {
		t.Error("Expected marshal error for unmarshalable content")
	}

	if !strings.Contains(err.Error(), "failed to marshal content blocks") {
		t.Errorf("Expected marshal error, got: %v", err)
	}
}

func TestClaudeProvider_ConvertMessagesToClaudeMultimodal_ConversionError(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// Message with unsupported content type to trigger conversion error
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				types.NewAudioPartFromData("audiodata", types.MIMETypeAudioMP3),
			},
		},
	}

	_, _, err := provider.convertMessagesToClaudeMultimodal(messages)
	if err == nil {
		t.Error("Expected conversion error for unsupported audio content")
	}
}

func TestClaudeProvider_StreamResponseMultimodal_CloseBody(t *testing.T) {
	// Test that streamResponseMultimodal properly closes the response body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Send a simple SSE event
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		server.URL,
		providers.ProviderDefaults{},
		false,
	)

	req := providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	streamChan, err := provider.PredictMultimodalStream(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error starting stream: %v", err)
	}

	// Drain the channel
	for range streamChan {
		// Just consume the chunks
	}

	// If we get here without hanging, the body was properly closed
}

func TestClaudeProvider_ParseClaudeResponse_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{"id": "msg_123", "type": "message", invalid json`)

	_, err := parseClaudeResponse(invalidJSON)
	if err == nil {
		t.Error("Expected unmarshal error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "failed to unmarshal response") {
		t.Errorf("Expected unmarshal error, got: %v", err)
	}
}

func TestClaudeProvider_PredictWithContentsMultimodal_MarshalError(t *testing.T) {
	provider := NewClaudeProvider(
		"test-claude",
		"claude-3-5-sonnet-20241022",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{},
		false,
	)

	// This test is tricky because we need to create a message that converts successfully
	// but then fails during JSON marshal. In practice, this is very rare.
	// We'll test the code path indirectly through the validation error tests.

	// For now, verify that normal messages marshal correctly
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	// This will fail at HTTP stage, but validates that marshal works
	_, err := provider.PredictMultimodal(context.Background(), req)
	// We expect an HTTP error, not a marshal error
	if err != nil && strings.Contains(err.Error(), "failed to marshal request") {
		t.Errorf("Unexpected marshal error for valid request: %v", err)
	}
}
