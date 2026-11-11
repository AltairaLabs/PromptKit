package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ============================================================================
// Tool result serialization on Chat Completions: the tool message carries
// text only, and an image follows the round in a user message.
// ============================================================================

// toolRoundMessages converts a round in which the model called render once and
// render returned parts.
func toolRoundMessages(t *testing.T, parts []types.ContentPart) ([]map[string]interface{}, error) {
	t.Helper()
	provider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil, nil)
	return provider.convertRequestMessagesToOpenAI(context.Background(), providers.PredictionRequest{Messages: []types.Message{
		{Role: "user", Content: "render it"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "call_1", Name: "render", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_1", Name: "render", Parts: parts}},
	}})
}

func TestToolResultText_IsTheToolMessageContent(t *testing.T) {
	cases := map[string]struct {
		parts []types.ContentPart
		want  string
	}{
		"one text part":   {[]types.ContentPart{types.NewTextPart(`{"temperature": 72}`)}, `{"temperature": 72}`},
		"two text parts":  {[]types.ContentPart{types.NewTextPart("Result 1. "), types.NewTextPart("Result 2.")}, "Result 1. Result 2."},
		"no parts at all": {nil, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			msgs, err := toolRoundMessages(t, tc.parts)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			if len(msgs) != 3 {
				t.Fatalf("Expected no message after a text-only result, got %d messages", len(msgs))
			}
			if msgs[2]["content"] != tc.want {
				t.Errorf("Expected %q, got %#v", tc.want, msgs[2]["content"])
			}
		})
	}
}

// An image-only result leaves the tool message saying where the image went.
func TestToolResultImageOnly_ToolMessageSaysWhereItWent(t *testing.T) {
	msgs, err := toolRoundMessages(t, []types.ContentPart{types.NewImagePartFromData("AAAA", types.MIMETypeImagePNG, nil)})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got := msgs[2]["content"]; got != "[The image this tool returned is attached in the next user message.]" {
		t.Errorf("Unexpected tool message content %#v", got)
	}
	if msgs[3]["role"] != "user" {
		t.Errorf("Expected the image in a user message, got role %v", msgs[3]["role"])
	}
}

// Audio on a model without audio input is described, not dropped.
func TestToolResultAudio_OnATextModelIsDescribed(t *testing.T) {
	msgs, err := toolRoundMessages(t, []types.ContentPart{
		types.NewTextPart("Transcription done"),
		types.NewAudioPartFromData("AAAA", types.MIMETypeAudioMP3),
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	want := "Transcription done\n[This tool returned an audio (audio/mpeg) that was not sent: " +
		"OpenAI Chat Completions does not accept audio input.]"
	if got := msgs[2]["content"]; got != want {
		t.Errorf("Expected %q, got %#v", want, got)
	}
	if len(msgs) != 3 {
		t.Errorf("Expected nothing attached, got %d messages", len(msgs))
	}
}

func TestToolResultImageURL_Base64(t *testing.T) {
	data := "AAAA"
	got, err := toolResultImageURL(t, &types.MediaContent{Data: &data, MIMEType: types.MIMETypeImagePNG})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expected := "data:image/png;base64,AAAA"; got != expected {
		t.Errorf("Expected %q, got %q", expected, got)
	}
}

func TestToolResultImageURL_URL(t *testing.T) {
	url := "https://example.com/img.jpg"
	got, err := toolResultImageURL(t, &types.MediaContent{URL: &url, MIMEType: types.MIMETypeImageJPEG})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != url {
		t.Errorf("Expected %q, got %q", url, got)
	}
}

// An image with no source fails the request: it used to be dropped from the
// tool result, so the model got the result without the image.
func TestToolResultImageURL_NoSourceIsAnError(t *testing.T) {
	if got, err := toolResultImageURL(t, &types.MediaContent{MIMEType: types.MIMETypeImagePNG}); err == nil {
		t.Errorf("Expected an error for an image with no source, got %q", got)
	}
}

// toolResultImageURL sends media as a tool result's only part and returns the
// image URL that goes on the wire, in the user message after the round.
func toolResultImageURL(t *testing.T, media *types.MediaContent) (string, error) {
	t.Helper()
	msgs, err := toolRoundMessages(t, []types.ContentPart{{Type: types.ContentTypeImage, Media: media}})
	if err != nil {
		return "", err
	}
	if len(msgs) != 4 {
		t.Fatalf("Expected the image in a fourth message, got %d messages", len(msgs))
	}
	parts, ok := msgs[3]["content"].([]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("Expected a label and the image, got %#v", msgs[3]["content"])
	}
	image, _ := parts[1].(map[string]interface{})["image_url"].(map[string]interface{})
	url, _ := image["url"].(string)
	return url, nil
}

// TestToolProvider_WithMultimodalMessages tests if tool calls work with multimodal messages
func TestToolProvider_WithMultimodalMessages(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil, nil)

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
	toolReq := mustBuildToolRequest(t, toolProvider, req, nil, "")

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

// TestToolProvider_BuildToolRequestWithLegacyMessage tests backward compatibility
func TestToolProvider_BuildToolRequestWithLegacyMessage(t *testing.T) {
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil, nil)

	// Legacy text-only message
	msg := types.Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	req := providers.PredictionRequest{
		Messages: []types.Message{msg},
	}

	toolReq := mustBuildToolRequest(t, toolProvider, req, nil, "")
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
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil, nil)

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

	toolReq := mustBuildToolRequest(t, toolProvider, req, tools, "auto")

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
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil, nil)

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

	toolReq := mustBuildToolRequest(t, toolProvider, req, nil, "")
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
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil, nil)

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

	toolReq := mustBuildToolRequest(t, toolProvider, req, nil, "")
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
	toolProvider := NewToolProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil, nil)

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
	_, err := toolProvider.convertMessageToOpenAI(context.Background(), msg)
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
