package openai

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ============================================================================
// Multimodal Tool Result Serialization Tests (#619)
// ============================================================================

func TestConvertToolResultContent_TextOnly(t *testing.T) {
	result := &types.MessageToolResult{
		ID:   "call_1",
		Name: "weather",
		Parts: []types.ContentPart{
			types.NewTextPart(`{"temperature": 72}`),
		},
	}

	content := convertToolResultContent(result)

	str, ok := content.(string)
	if !ok {
		t.Fatalf("Expected string for text-only result, got %T", content)
	}
	if str != `{"temperature": 72}` {
		t.Errorf("Expected tool result text, got %q", str)
	}
}

func TestConvertToolResultContent_MultipleTextParts(t *testing.T) {
	result := &types.MessageToolResult{
		ID:   "call_2",
		Name: "search",
		Parts: []types.ContentPart{
			types.NewTextPart("Result 1. "),
			types.NewTextPart("Result 2."),
		},
	}

	content := convertToolResultContent(result)

	// Multiple text-only parts still return concatenated string
	str, ok := content.(string)
	if !ok {
		t.Fatalf("Expected string for text-only result, got %T", content)
	}
	if str != "Result 1. Result 2." {
		t.Errorf("Expected concatenated text, got %q", str)
	}
}

func TestConvertToolResultContent_Base64Image(t *testing.T) {
	b64Data := "iVBORw0KGgoAAAANSUhEUg=="
	result := &types.MessageToolResult{
		ID:   "call_3",
		Name: "chart_gen",
		Parts: []types.ContentPart{
			types.NewTextPart("Chart generated successfully"),
			types.NewImagePartFromData(b64Data, types.MIMETypeImagePNG, nil),
		},
	}

	content := convertToolResultContent(result)

	parts, ok := content.([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected []map[string]interface{} for multimodal result, got %T", content)
	}
	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	// Verify text part
	if parts[0]["type"] != "text" {
		t.Errorf("Expected first part type 'text', got %v", parts[0]["type"])
	}
	if parts[0]["text"] != "Chart generated successfully" {
		t.Errorf("Expected text content, got %v", parts[0]["text"])
	}

	// Verify image part
	if parts[1]["type"] != "image_url" {
		t.Errorf("Expected second part type 'image_url', got %v", parts[1]["type"])
	}
	imgURL, ok := parts[1]["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image_url to be a map, got %T", parts[1]["image_url"])
	}
	expectedURL := "data:image/png;base64," + b64Data
	if imgURL["url"] != expectedURL {
		t.Errorf("Expected data URI %q, got %v", expectedURL, imgURL["url"])
	}
}

func TestConvertToolResultContent_URLImage(t *testing.T) {
	imgURL := "https://example.com/chart.png"
	result := &types.MessageToolResult{
		ID:   "call_4",
		Name: "screenshot",
		Parts: []types.ContentPart{
			types.NewTextPart("Screenshot captured"),
			types.NewImagePartFromURL(imgURL, nil),
		},
	}

	content := convertToolResultContent(result)

	parts, ok := content.([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected []map[string]interface{} for multimodal result, got %T", content)
	}
	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	// Verify image part URL is passed through
	imgBlock, ok := parts[1]["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image_url map, got %T", parts[1]["image_url"])
	}
	if imgBlock["url"] != imgURL {
		t.Errorf("Expected URL %q, got %v", imgURL, imgBlock["url"])
	}
}

func TestConvertToolResultContent_ImageOnly(t *testing.T) {
	b64Data := "AAAA"
	result := &types.MessageToolResult{
		ID:   "call_5",
		Name: "render",
		Parts: []types.ContentPart{
			types.NewImagePartFromData(b64Data, types.MIMETypeImageJPEG, nil),
		},
	}

	content := convertToolResultContent(result)

	parts, ok := content.([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected []map[string]interface{} for image-only result, got %T", content)
	}
	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}
	if parts[0]["type"] != "image_url" {
		t.Errorf("Expected type 'image_url', got %v", parts[0]["type"])
	}
}

func TestConvertToolResultContent_MixedContent(t *testing.T) {
	b64Data := "AAAA"
	imgURL := "https://example.com/plot.png"
	result := &types.MessageToolResult{
		ID:   "call_6",
		Name: "analysis",
		Parts: []types.ContentPart{
			types.NewTextPart("Analysis complete"),
			types.NewImagePartFromData(b64Data, types.MIMETypeImagePNG, nil),
			types.NewTextPart("See charts above"),
			types.NewImagePartFromURL(imgURL, nil),
		},
	}

	content := convertToolResultContent(result)

	parts, ok := content.([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected []map[string]interface{}, got %T", content)
	}
	if len(parts) != 4 {
		t.Fatalf("Expected 4 parts, got %d", len(parts))
	}

	// Verify ordering and types
	expectedTypes := []string{"text", "image_url", "text", "image_url"}
	for i, expected := range expectedTypes {
		if parts[i]["type"] != expected {
			t.Errorf("Part %d: expected type %q, got %v", i, expected, parts[i]["type"])
		}
	}
}

func TestConvertToolResultContent_EmptyParts(t *testing.T) {
	result := &types.MessageToolResult{
		ID:    "call_7",
		Name:  "noop",
		Parts: nil,
	}

	content := convertToolResultContent(result)

	// No parts means text-only path returns empty string
	str, ok := content.(string)
	if !ok {
		t.Fatalf("Expected string for empty result, got %T", content)
	}
	if str != "" {
		t.Errorf("Expected empty string, got %q", str)
	}
}

func TestConvertToolResultContent_UnsupportedMediaSkipped(t *testing.T) {
	audioData := "AAAA"
	result := &types.MessageToolResult{
		ID:   "call_8",
		Name: "transcribe",
		Parts: []types.ContentPart{
			types.NewTextPart("Transcription done"),
			types.NewAudioPartFromData(audioData, types.MIMETypeAudioMP3),
		},
	}

	content := convertToolResultContent(result)

	// HasMedia() returns true, so we get array format, but audio is skipped
	parts, ok := content.([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected []map[string]interface{}, got %T", content)
	}
	// Only text part should remain (audio is skipped)
	if len(parts) != 1 {
		t.Fatalf("Expected 1 part (audio skipped), got %d", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("Expected text part, got %v", parts[0]["type"])
	}
}

// TestConvertSingleMessageForTools_MultimodalToolResult verifies end-to-end conversion
// of a tool result message with multimodal content through convertSingleMessageForTools.
func TestConvertSingleMessageForTools_MultimodalToolResult(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	b64Data := "iVBORw0KGgoAAAANSUhEUg=="
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "call_100",
			Name: "chart_gen",
			Parts: []types.ContentPart{
				types.NewTextPart("Chart generated"),
				types.NewImagePartFromData(b64Data, types.MIMETypeImagePNG, nil),
			},
		},
	}

	openaiMsg := provider.convertSingleMessageForTools(msg)

	if openaiMsg["tool_call_id"] != "call_100" {
		t.Errorf("Expected tool_call_id 'call_100', got %v", openaiMsg["tool_call_id"])
	}
	if openaiMsg["name"] != "chart_gen" {
		t.Errorf("Expected name 'chart_gen', got %v", openaiMsg["name"])
	}

	// Content should be an array (multimodal)
	parts, ok := openaiMsg["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected multimodal content array, got %T", openaiMsg["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	// Verify JSON serialization is valid
	data, err := json.Marshal(openaiMsg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if parsed["role"] != "tool" {
		t.Errorf("Expected role 'tool', got %v", parsed["role"])
	}
}

func TestResolveImageURL_Base64(t *testing.T) {
	data := "AAAA"
	media := &types.MediaContent{
		Data:     &data,
		MIMEType: types.MIMETypeImagePNG,
	}
	got := resolveImageURL(media)
	expected := "data:image/png;base64,AAAA"
	if got != expected {
		t.Errorf("Expected %q, got %q", expected, got)
	}
}

func TestResolveImageURL_URL(t *testing.T) {
	url := "https://example.com/img.jpg"
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: types.MIMETypeImageJPEG,
	}
	got := resolveImageURL(media)
	if got != url {
		t.Errorf("Expected %q, got %q", url, got)
	}
}

func TestResolveImageURL_Empty(t *testing.T) {
	media := &types.MediaContent{
		MIMEType: types.MIMETypeImagePNG,
	}
	got := resolveImageURL(media)
	if got != "" {
		t.Errorf("Expected empty string, got %q", got)
	}
}

// TestToolProvider_WithMultimodalMessages tests if tool calls work with multimodal messages
func TestToolProvider_WithMultimodalMessages(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Create a multimodal message with an image
	imageURL := "https://example.com/chart.png"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("What's the trend in this chart?"),
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &imageURL,
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	// Build a request with this multimodal message
	req := providers.PredictionRequest{
		Messages: []types.Message{msg},
	}

	// The current buildToolRequest uses msg.Content directly
	// This will NOT work correctly with multimodal messages
	toolReq := toolProvider.buildToolRequest(req, nil, "")

	// Extract the message that was built
	messages, ok := toolReq["messages"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected messages to be []map[string]interface{}")
	}

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	// Check what content was set
	content := messages[0]["content"]

	// After fix: Should handle multimodal messages correctly
	if content == "" {
		t.Error("Content should not be empty for multimodal messages")
	}

	// For multimodal messages, content should be []interface{} (array of parts)
	switch v := content.(type) {
	case []interface{}:
		t.Log("✓ Content is correctly formatted as multimodal parts")
		if len(v) != 2 {
			t.Errorf("Expected 2 content parts (text + image), got %d", len(v))
		}

		// Verify text part
		if textPart, ok := v[0].(map[string]interface{}); ok {
			if textPart["type"] == "text" {
				t.Log("✓ Text part present:", textPart["text"])
			}
		}

		// Verify image part
		if imagePart, ok := v[1].(map[string]interface{}); ok {
			if imagePart["type"] == "image_url" {
				t.Log("✓ Image part present")
			}
		}
	case string:
		// Fallback to legacy format (shouldn't happen with multimodal messages)
		t.Error("Content is string (legacy format), expected multimodal array format")
	default:
		t.Errorf("Unexpected content type: %T", content)
	}

	t.Logf("Content type: %T", content)
}

// TestToolProvider_MultimodalToolSupport checks if the interface is implemented
func TestToolProvider_MultimodalToolSupport(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Check if it implements providers.MultimodalToolSupport interface
	_, ok := interface{}(toolProvider).(providers.MultimodalToolSupport)
	if !ok {
		t.Error("ToolProvider should implement providers.MultimodalToolSupport interface")
		t.Log("    Missing: PredictMultimodalWithTools() method")
	} else {
		t.Log("✓ ToolProvider implements providers.MultimodalToolSupport interface")
	}

	// Check if it at least implements providers.MultimodalSupport
	_, ok = interface{}(toolProvider).(providers.MultimodalSupport)
	if ok {
		t.Log("✓ ToolProvider implements providers.MultimodalSupport interface")
		t.Log("    This means it inherits GetMultimodalCapabilities(), PredictMultimodal(), PredictMultimodalStream()")
	} else {
		t.Log("⚠️  ToolProvider does NOT implement providers.MultimodalSupport interface")
	}
}

// TestOpenAIProvider_InheritsMultimodal verifies base provider has multimodal support
func TestOpenAIProvider_InheritsMultimodal(t *testing.T) {
	baseProvider := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	// Check if base provider implements providers.MultimodalSupport
	_, ok := interface{}(baseProvider).(providers.MultimodalSupport)
	if !ok {
		t.Fatal("OpenAIProvider should implement providers.MultimodalSupport interface")
	}

	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Since ToolProvider embeds *Provider, it should inherit the interface
	_, ok = interface{}(toolProvider).(providers.MultimodalSupport)
	if !ok {
		t.Fatal("ToolProvider should inherit providers.MultimodalSupport from OpenAIProvider")
	}

	t.Log("✓ ToolProvider inherits providers.MultimodalSupport from OpenAIProvider")
}

// TestToolProvider_BuildToolRequestWithLegacyMessage tests backward compatibility
func TestToolProvider_BuildToolRequestWithLegacyMessage(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Legacy text-only message
	msg := types.Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	req := providers.PredictionRequest{
		Messages: []types.Message{msg},
	}

	toolReq := toolProvider.buildToolRequest(req, nil, "")
	messages := toolReq["messages"].([]map[string]interface{})

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	content := messages[0]["content"]
	if contentStr, ok := content.(string); ok {
		if contentStr != "Hello, world!" {
			t.Errorf("Expected content 'Hello, world!', got '%s'", contentStr)
		}
		t.Log("✓ Legacy text-only messages work correctly")
	} else {
		t.Errorf("Expected string content for legacy message, got %T", content)
	}
}

// TestToolProvider_BuildToolRequestWithTools tests message conversion with actual tools
func TestToolProvider_BuildToolRequestWithTools(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	imageURL := "https://example.com/chart.png"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Analyze this chart and extract the data"),
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &imageURL,
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	req := providers.PredictionRequest{
		Messages: []types.Message{msg},
	}

	// Create mock tools
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "extract_data",
				"description": "Extract data from a chart",
			},
		},
	}

	toolReq := toolProvider.buildToolRequest(req, tools, "auto")

	// Verify tools are present
	if toolReq["tools"] == nil {
		t.Error("Expected tools to be present in request")
	}

	// Verify message has multimodal content
	messages := toolReq["messages"].([]map[string]interface{})
	content := messages[0]["content"]

	if parts, ok := content.([]interface{}); ok {
		if len(parts) != 2 {
			t.Errorf("Expected 2 content parts, got %d", len(parts))
		}
		t.Log("✓ Multimodal content preserved when using tools")
	} else {
		t.Error("Expected multimodal content format")
	}
}

// TestToolProvider_BuildToolRequestWithToolCalls tests messages with tool call responses
func TestToolProvider_BuildToolRequestWithToolCalls(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Message with tool calls
	assistantMsg := types.Message{
		Role:    "assistant",
		Content: "I'll analyze the chart for you.",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_123",
				Name: "extract_data",
				Args: []byte(`{"chart_type":"bar"}`),
			},
		},
	}

	// Tool result message
	toolResultMsg := types.Message{
		Role:    "tool",
		Content: `{"data": [1,2,3,4,5]}`,
		ToolResult: &types.MessageToolResult{
			ID:   "call_123",
			Name: "extract_data",
		},
	}

	req := providers.PredictionRequest{
		Messages: []types.Message{assistantMsg, toolResultMsg},
	}

	toolReq := toolProvider.buildToolRequest(req, nil, "")
	messages := toolReq["messages"].([]map[string]interface{})

	if len(messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(messages))
	}

	// Check assistant message has tool_calls
	if messages[0]["tool_calls"] == nil {
		t.Error("Expected tool_calls in assistant message")
	} else {
		t.Log("✓ Tool calls preserved in message")
	}

	// Check tool result message has tool_call_id
	if messages[1]["tool_call_id"] == nil {
		t.Error("Expected tool_call_id in tool result message")
	} else {
		t.Log("✓ Tool result properly formatted")
	}
}

// TestToolProvider_BuildToolRequestWithMultimodalAndToolResult tests complex scenario
func TestToolProvider_BuildToolRequestWithMultimodalAndToolResult(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	imageURL := "https://example.com/chart.png"

	// User sends multimodal message
	userMsg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Analyze this chart"),
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &imageURL,
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	// Assistant responds with tool call
	assistantMsg := types.Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_456",
				Name: "analyze_chart",
				Args: []byte(`{}`),
			},
		},
	}

	// Tool returns result
	toolMsg := types.Message{
		Role:    "tool",
		Content: `{"trend":"upward"}`,
		ToolResult: &types.MessageToolResult{
			ID:   "call_456",
			Name: "analyze_chart",
		},
	}

	// Assistant's final response (could be multimodal too)
	finalMsg := types.Message{
		Role:    "assistant",
		Content: "The chart shows an upward trend.",
	}

	req := providers.PredictionRequest{
		Messages: []types.Message{userMsg, assistantMsg, toolMsg, finalMsg},
	}

	toolReq := toolProvider.buildToolRequest(req, nil, "")
	messages := toolReq["messages"].([]map[string]interface{})

	if len(messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(messages))
	}

	// Verify first message is multimodal
	if parts, ok := messages[0]["content"].([]interface{}); ok {
		if len(parts) != 2 {
			t.Errorf("Expected 2 content parts in user message, got %d", len(parts))
		}
		t.Log("✓ User's multimodal message preserved in conversation with tools")
	} else {
		t.Error("Expected multimodal content in first message")
	}

	// Verify tool call message
	if messages[1]["tool_calls"] == nil {
		t.Error("Expected tool_calls in assistant message")
	}

	// Verify tool result message
	if messages[2]["tool_call_id"] == nil {
		t.Error("Expected tool_call_id in tool result message")
	}

	// Verify final message
	if messages[3]["content"] == "" {
		t.Error("Expected content in final assistant message")
	}

	t.Log("✓ Complex multimodal + tool conversation handled correctly")
}

// TestToolProvider_MultimodalValidation tests validation before tool calls
func TestToolProvider_MultimodalValidation(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Create unsupported media type (audio)
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

	// This should fail validation since OpenAI doesn't support audio in predict
	_, err := toolProvider.convertMessageToOpenAI(msg)
	if err == nil {
		t.Error("Expected error for unsupported audio content")
	} else {
		t.Log("✓ Validation correctly rejects unsupported media types")
	}

	// Verify providers.ValidateMultimodalMessage also catches this
	err = providers.ValidateMultimodalMessage(toolProvider, msg)
	if err == nil {
		t.Error("Expected providers.ValidateMultimodalMessage to reject audio")
	} else {
		t.Log("✓ providers.ValidateMultimodalMessage correctly rejects audio:", err)
	}
}
