package vllm

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/testutil"
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
