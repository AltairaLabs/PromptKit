package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestProvider_GetMultimodalCapabilities(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	caps := provider.GetMultimodalCapabilities()

	if !caps.SupportsImages {
		t.Error("Expected SupportsImages to be true")
	}

	if caps.SupportsAudio {
		t.Error("Expected SupportsAudio to be false")
	}

	if caps.SupportsVideo {
		t.Error("Expected SupportsVideo to be false")
	}

	expectedFormats := []string{
		types.MIMETypeImageJPEG,
		types.MIMETypeImagePNG,
		types.MIMETypeImageGIF,
		types.MIMETypeImageWebP,
	}

	if len(caps.ImageFormats) != len(expectedFormats) {
		t.Errorf("Expected %d image formats, got %d", len(expectedFormats), len(caps.ImageFormats))
	}

	for i, format := range expectedFormats {
		if caps.ImageFormats[i] != format {
			t.Errorf("Expected format %s at index %d, got %s", format, i, caps.ImageFormats[i])
		}
	}
}

func TestProvider_ConvertMessageToOllama_TextOnly(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	msg := &types.Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	result, err := provider.convertMessageToOllama(msg)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", result.Role)
	}

	content, ok := result.Content.(string)
	if !ok {
		t.Fatal("Expected content to be string")
	}

	if content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", content)
	}
}

func TestProvider_ConvertMessageToOllama_Multimodal(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	text := "What's in this image?"
	imageURL := "https://example.com/image.jpg"

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &text},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &imageURL,
					MIMEType: types.MIMETypeImageJPEG,
				},
			},
		},
	}

	result, err := provider.convertMessageToOllama(msg)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", result.Role)
	}

	parts, ok := result.Content.([]any)
	if !ok {
		t.Fatal("Expected content to be []any for multimodal")
	}

	if len(parts) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(parts))
	}
}

func TestProvider_ConvertMessageToOllama_UnsupportedMedia(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	text := "What's in this audio?"

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &text},
			{Type: types.ContentTypeAudio, Media: &types.MediaContent{}},
		},
	}

	_, err := provider.convertMessageToOllama(msg)

	if err == nil {
		t.Error("Expected error for unsupported audio content")
	}
}

func TestProvider_ConvertMessageToOllama_MissingMediaContent(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	text := "What's in this image?"

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &text},
			{Type: types.ContentTypeImage, Media: nil},
		},
	}

	_, err := provider.convertMessageToOllama(msg)

	if err == nil {
		t.Error("Expected error for missing media content")
	}
}

func TestProvider_ConvertImagePartToOllama_URL(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	imageURL := "https://example.com/image.jpg"
	detail := "high"

	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			URL:      &imageURL,
			MIMEType: types.MIMETypeImageJPEG,
			Detail:   &detail,
		},
	}

	result, err := provider.convertImagePartToOllama(part)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result["type"] != "image_url" {
		t.Errorf("Expected type 'image_url', got '%v'", result["type"])
	}

	imageURLMap, ok := result["image_url"].(map[string]any)
	if !ok {
		t.Fatal("Expected image_url to be map")
	}

	if imageURLMap["url"] != imageURL {
		t.Errorf("Expected URL '%s', got '%v'", imageURL, imageURLMap["url"])
	}

	if imageURLMap["detail"] != "high" {
		t.Errorf("Expected detail 'high', got '%v'", imageURLMap["detail"])
	}
}

func TestProvider_ConvertImagePartToOllama_MissingMedia(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	part := types.ContentPart{
		Type:  types.ContentTypeImage,
		Media: nil,
	}

	_, err := provider.convertImagePartToOllama(part)

	if err == nil {
		t.Error("Expected error for missing media")
	}
}

func TestProvider_PredictMultimodal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "I see a cat in the image."}},
			},
			"usage": map[string]any{"prompt_tokens": 50, "completion_tokens": 10},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "llava:13b", server.URL,
		providers.ProviderDefaults{MaxTokens: 1000}, false, nil)

	text := "What's in this image?"
	imageURL := "https://example.com/cat.jpg"

	resp, err := provider.PredictMultimodal(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role: "user",
				Parts: []types.ContentPart{
					{Type: types.ContentTypeText, Text: &text},
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							URL:      &imageURL,
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if resp.Content != "I see a cat in the image." {
		t.Errorf("Expected content about cat, got '%s'", resp.Content)
	}
}

func TestProvider_ConvertMessagesToOllama(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	req := providers.PredictionRequest{
		System: "You are a helpful assistant.",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		},
	}

	messages, err := provider.convertMessagesToOllama(req)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// System + 3 messages
	if len(messages) != 4 {
		t.Errorf("Expected 4 messages, got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Errorf("Expected first message to be system, got '%s'", messages[0].Role)
	}

	if messages[0].Content != "You are a helpful assistant." {
		t.Errorf("System content mismatch")
	}
}

func TestToolProvider_PredictMultimodalWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":       "call_1",
								"type":     "function",
								"function": map[string]any{"name": "analyze_image", "arguments": "{}"},
							},
						},
					},
				},
			},
			"usage": map[string]any{"prompt_tokens": 50, "completion_tokens": 10},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewToolProvider("test", "llava:13b", server.URL,
		providers.ProviderDefaults{MaxTokens: 1000}, false, nil)

	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{Name: "analyze_image", Description: "Analyze image", InputSchema: json.RawMessage(`{}`)},
	})

	text := "Analyze this image"
	imageURL := "https://example.com/image.jpg"

	_, toolCalls, err := provider.PredictMultimodalWithTools(
		context.Background(),
		providers.PredictionRequest{
			Messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						{Type: types.ContentTypeText, Text: &text},
						{Type: types.ContentTypeImage, Media: &types.MediaContent{
							URL:      &imageURL,
							MIMEType: types.MIMETypeImageJPEG,
						}},
					},
				},
			},
		},
		tools,
		"auto",
	)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(toolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(toolCalls))
	}
}

func TestProvider_ConvertMessageToOllama_UnknownContentType(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	text := "Test"

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &text},
			{Type: "unknown_type", Media: &types.MediaContent{}},
		},
	}

	_, err := provider.convertMessageToOllama(msg)

	if err == nil {
		t.Error("Expected error for unknown content type")
	}
}

func TestProvider_ConvertMessageToOllama_VideoUnsupported(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	text := "What's in this video?"

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &text},
			{Type: types.ContentTypeVideo, Media: &types.MediaContent{}},
		},
	}

	_, err := provider.convertMessageToOllama(msg)

	if err == nil {
		t.Error("Expected error for unsupported video content")
	}
}

func TestProvider_ConvertMessageToOllama_EmptyTextPart(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	emptyText := ""
	imageURL := "https://example.com/image.jpg"

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &emptyText},
			{Type: types.ContentTypeImage, Media: &types.MediaContent{
				URL:      &imageURL,
				MIMEType: types.MIMETypeImageJPEG,
			}},
		},
	}

	result, err := provider.convertMessageToOllama(msg)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	parts, ok := result.Content.([]any)
	if !ok {
		t.Fatal("Expected content to be []any")
	}

	// Empty text part should be skipped, so only 1 part (image)
	if len(parts) != 1 {
		t.Errorf("Expected 1 part (empty text skipped), got %d", len(parts))
	}
}

func TestProvider_PredictMultimodalStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("Streaming not supported")
		}

		chunks := []string{
			`{"choices":[{"delta":{"content":"I "}}]}`,
			`{"choices":[{"delta":{"content":"see a cat"}}]}`,
			`{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":5}}`,
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewProvider("test", "llava:13b", server.URL,
		providers.ProviderDefaults{MaxTokens: 1000}, false, nil)

	text := "What's in this image?"
	imageURL := "https://example.com/cat.jpg"

	ch, err := provider.PredictMultimodalStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role: "user",
				Parts: []types.ContentPart{
					{Type: types.ContentTypeText, Text: &text},
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							URL:      &imageURL,
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	var lastContent string
	for chunk := range ch {
		if chunk.Content != "" {
			lastContent = chunk.Content
		}
	}

	if lastContent != "I see a cat" {
		t.Errorf("Expected 'I see a cat', got '%s'", lastContent)
	}
}

func TestProvider_PredictMultimodalStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	provider := NewProvider("test", "llava:13b", server.URL,
		providers.ProviderDefaults{MaxTokens: 100}, false, nil)

	text := "What's in this image?"
	imageURL := "https://example.com/image.jpg"

	_, err := provider.PredictMultimodalStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role: "user",
				Parts: []types.ContentPart{
					{Type: types.ContentTypeText, Text: &text},
					{Type: types.ContentTypeImage, Media: &types.MediaContent{
						URL:      &imageURL,
						MIMEType: types.MIMETypeImageJPEG,
					}},
				},
			},
		},
	})

	if err == nil {
		t.Error("Expected error for server error")
	}
}

func TestProvider_ConvertImagePartToOllama_Base64(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	base64Data := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			Data:     &base64Data,
			MIMEType: types.MIMETypeImagePNG,
		},
	}

	result, err := provider.convertImagePartToOllama(part)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result["type"] != "image_url" {
		t.Errorf("Expected type 'image_url', got '%v'", result["type"])
	}

	imageURLMap, ok := result["image_url"].(map[string]any)
	if !ok {
		t.Fatal("Expected image_url to be map")
	}

	expectedURL := "data:image/png;base64," + base64Data
	if imageURLMap["url"] != expectedURL {
		t.Errorf("Expected data URL, got '%v'", imageURLMap["url"])
	}
}

func TestProvider_ConvertImagePartToOllama_MissingURLAndData(t *testing.T) {
	provider := NewProvider("test", "llava:13b", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: types.MIMETypeImagePNG,
		},
	}

	_, err := provider.convertImagePartToOllama(part)

	if err == nil {
		t.Error("Expected error for missing URL and data")
	}
}
