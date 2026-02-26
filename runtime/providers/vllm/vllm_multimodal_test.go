package vllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestGetMultimodalCapabilities(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

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
	if len(caps.ImageFormats) != 4 {
		t.Errorf("Expected 4 image formats, got %d", len(caps.ImageFormats))
	}
	if caps.MaxImageSizeMB != 20 {
		t.Errorf("Expected MaxImageSizeMB=20, got %d", caps.MaxImageSizeMB)
	}
}

func TestPredictMultimodal_WithImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vllmRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify multimodal format
		if len(req.Messages) == 0 {
			t.Error("Expected messages")
			return
		}

		// Check that message content is an array (multimodal format)
		msg := req.Messages[0]
		contentArray, ok := msg.Content.([]interface{})
		if !ok {
			t.Errorf("Expected content to be array for multimodal, got %T", msg.Content)
		} else if len(contentArray) != 2 {
			t.Errorf("Expected 2 content parts, got %d", len(contentArray))
		}

		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "I see an image"}},
			},
			Usage: vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	// Create multimodal message with image
	msg := types.Message{Role: "user"}
	msg.AddTextPart("What's in this image?")
	imageURL := "https://example.com/image.jpg"
	msg.AddImagePartFromURL(imageURL, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{msg},
	}

	_, err := provider.PredictMultimodal(context.Background(), req)
	if err != nil {
		t.Errorf("PredictMultimodal failed: %v", err)
	}
}

func TestPredictMultimodal_WithBase64Image(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vllmRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "I see an image"}},
			},
			Usage: vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	// Create message with base64 image
	msg := types.Message{Role: "user"}
	msg.AddTextPart("What's in this image?")
	base64Data := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	imagePart := types.NewImagePartFromData(base64Data, "image/png", nil)
	msg.AddPart(imagePart)

	req := providers.PredictionRequest{
		Messages: []types.Message{msg},
	}

	_, err := provider.PredictMultimodal(context.Background(), req)
	if err != nil {
		t.Errorf("PredictMultimodal with base64 image failed: %v", err)
	}
}

func TestPredictMultimodal_TextOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vllmRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify text-only format (string, not array)
		if len(req.Messages) > 0 {
			msg := req.Messages[0]
			if _, ok := msg.Content.(string); !ok {
				t.Errorf("Expected string content for text-only message, got %T", msg.Content)
			}
		}

		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "Text response"}},
			},
			Usage: vllmUsage{PromptTokens: 5, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Hello"}},
	}

	_, err := provider.PredictMultimodal(context.Background(), req)
	if err != nil {
		t.Errorf("PredictMultimodal with text-only failed: %v", err)
	}
}

func TestPredictMultimodalStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"choices":[{"delta":{"content":"I see"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":" an image"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	msg := types.Message{Role: "user"}
	msg.AddTextPart("What's in this image?")
	msg.AddImagePartFromURL("https://example.com/image.jpg", nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{msg},
	}

	stream, err := provider.PredictMultimodalStream(context.Background(), req)
	if err != nil {
		t.Fatalf("PredictMultimodalStream failed: %v", err)
	}

	var content string
	for chunk := range stream {
		if chunk.Error != nil {
			t.Errorf("Stream chunk error: %v", chunk.Error)
		}
		content = chunk.Content
	}

	if content != "I see an image" {
		t.Errorf("Expected 'I see an image', got '%s'", content)
	}
}

func TestBuildMultimodalContent_TextAndImage(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	msg := types.Message{Role: "user"}
	msg.AddTextPart("Describe this")
	msg.AddImagePartFromURL("https://example.com/test.jpg", nil)

	content, err := provider.buildMultimodalContent(msg)
	if err != nil {
		t.Fatalf("buildMultimodalContent failed: %v", err)
	}

	if len(content) != 2 {
		t.Errorf("Expected 2 content parts, got %d", len(content))
	}

	// Check text part
	textPart := content[0]
	if textPart["type"] != "text" {
		t.Errorf("Expected text type, got %v", textPart["type"])
	}
	if textPart["text"] != "Describe this" {
		t.Errorf("Expected text 'Describe this', got %v", textPart["text"])
	}

	// Check image part
	imagePart := content[1]
	if imagePart["type"] != "image_url" {
		t.Errorf("Expected image_url type, got %v", imagePart["type"])
	}
}

func TestBuildMultimodalContent_WithDetail(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	msg := types.Message{Role: "user"}
	msg.AddTextPart("Test")
	detail := "high"
	msg.AddImagePartFromURL("https://example.com/test.jpg", &detail)

	content, err := provider.buildMultimodalContent(msg)
	if err != nil {
		t.Fatalf("buildMultimodalContent failed: %v", err)
	}

	imagePart := content[1]
	imageURL := imagePart["image_url"].(map[string]any)
	if imageURL["detail"] != "high" {
		t.Errorf("Expected detail 'high', got %v", imageURL["detail"])
	}
}

func TestBuildMultimodalContent_UnsupportedType(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	// Create message with audio part (unsupported)
	msg := types.Message{Role: "user"}
	audioPart := types.ContentPart{
		Type: types.ContentTypeAudio,
		Media: &types.MediaContent{
			MIMEType: "audio/mp3",
			Data:     testutil.Ptr("base64data"),
		},
	}
	msg.Parts = []types.ContentPart{audioPart}

	content, err := provider.buildMultimodalContent(msg)
	if err != nil {
		t.Fatalf("buildMultimodalContent failed: %v", err)
	}

	// Audio should be skipped
	if len(content) != 0 {
		t.Errorf("Expected audio to be skipped, got %d parts", len(content))
	}
}

func TestConvertMediaToURL_URL(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	url := "https://example.com/image.jpg"
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "image/jpeg",
	}

	result, err := provider.convertMediaToURL(media)
	if err != nil {
		t.Fatalf("convertMediaToURL failed: %v", err)
	}

	if result != url {
		t.Errorf("Expected %s, got %s", url, result)
	}
}

func TestConvertMediaToURL_Base64Data(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	data := "base64encodeddata"
	media := &types.MediaContent{
		Data:     &data,
		MIMEType: "image/png",
	}

	result, err := provider.convertMediaToURL(media)
	if err != nil {
		t.Fatalf("convertMediaToURL failed: %v", err)
	}

	expected := "data:image/png;base64,base64encodeddata"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestConvertMediaToURL_FilePath(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	filePath := "/path/to/image.jpg"
	media := &types.MediaContent{
		FilePath: &filePath,
		MIMEType: "image/jpeg",
	}

	_, err := provider.convertMediaToURL(media)
	if err == nil {
		t.Error("Expected error for file path without loaded data")
	}
}

func TestConvertMediaToURL_NoSource(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	media := &types.MediaContent{
		MIMEType: "image/jpeg",
	}

	_, err := provider.convertMediaToURL(media)
	if err == nil {
		t.Error("Expected error for media with no source")
	}
}

func TestPrepareMultimodalMessages_WithSystem(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	msg := types.Message{Role: "user"}
	msg.AddTextPart("Test")
	msg.AddImagePartFromURL("https://example.com/test.jpg", nil)

	req := providers.PredictionRequest{
		System:   "You are a helpful assistant",
		Messages: []types.Message{msg},
	}

	messages, err := provider.prepareMultimodalMessages(req)
	if err != nil {
		t.Fatalf("prepareMultimodalMessages failed: %v", err)
	}

	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("Expected first message to be system, got %s", messages[0].Role)
	}
}
