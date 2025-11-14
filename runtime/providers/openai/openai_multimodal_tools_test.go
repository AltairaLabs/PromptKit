package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestOpenAIToolProvider_WithMultimodalMessages tests if tool calls work with multimodal messages
func TestOpenAIToolProvider_WithMultimodalMessages(t *testing.T) {
	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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

// TestOpenAIToolProvider_MultimodalToolSupport checks if the interface is implemented
func TestOpenAIToolProvider_MultimodalToolSupport(t *testing.T) {
	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Check if it implements providers.MultimodalToolSupport interface
	_, ok := interface{}(toolProvider).(providers.MultimodalToolSupport)
	if !ok {
		t.Error("OpenAIToolProvider should implement providers.MultimodalToolSupport interface")
		t.Log("    Missing: PredictMultimodalWithTools() method")
	} else {
		t.Log("✓ OpenAIToolProvider implements providers.MultimodalToolSupport interface")
	}

	// Check if it at least implements providers.MultimodalSupport
	_, ok = interface{}(toolProvider).(providers.MultimodalSupport)
	if ok {
		t.Log("✓ OpenAIToolProvider implements providers.MultimodalSupport interface")
		t.Log("    This means it inherits GetMultimodalCapabilities(), PredictMultimodal(), PredictMultimodalStream()")
	} else {
		t.Log("⚠️  OpenAIToolProvider does NOT implement providers.MultimodalSupport interface")
	}
}

// TestOpenAIProvider_InheritsMultimodal verifies base provider has multimodal support
func TestOpenAIProvider_InheritsMultimodal(t *testing.T) {
	baseProvider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	// Check if base provider implements providers.MultimodalSupport
	_, ok := interface{}(baseProvider).(providers.MultimodalSupport)
	if !ok {
		t.Fatal("OpenAIProvider should implement providers.MultimodalSupport interface")
	}

	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Since OpenAIToolProvider embeds *OpenAIProvider, it should inherit the interface
	_, ok = interface{}(toolProvider).(providers.MultimodalSupport)
	if !ok {
		t.Fatal("OpenAIToolProvider should inherit providers.MultimodalSupport from OpenAIProvider")
	}

	t.Log("✓ OpenAIToolProvider inherits providers.MultimodalSupport from OpenAIProvider")
}

// TestOpenAIToolProvider_BuildToolRequestWithLegacyMessage tests backward compatibility
func TestOpenAIToolProvider_BuildToolRequestWithLegacyMessage(t *testing.T) {
	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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

// TestOpenAIToolProvider_BuildToolRequestWithTools tests message conversion with actual tools
func TestOpenAIToolProvider_BuildToolRequestWithTools(t *testing.T) {
	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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

// TestOpenAIToolProvider_BuildToolRequestWithToolCalls tests messages with tool call responses
func TestOpenAIToolProvider_BuildToolRequestWithToolCalls(t *testing.T) {
	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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

// TestOpenAIToolProvider_BuildToolRequestWithMultimodalAndToolResult tests complex scenario
func TestOpenAIToolProvider_BuildToolRequestWithMultimodalAndToolResult(t *testing.T) {
	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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

// TestOpenAIToolProvider_MultimodalValidation tests validation before tool calls
func TestOpenAIToolProvider_MultimodalValidation(t *testing.T) {
	toolProvider := NewOpenAIToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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

	// This should fail validation since OpenAI doesn't support audio in chat
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
