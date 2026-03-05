package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestToolProvider_BuildRequestWithToolMessages tests that tool messages
// from conversation history are properly converted to Gemini's functionResponse format
func TestToolProvider_BuildRequestWithToolMessages(t *testing.T) {
	// Track what gets sent to Gemini API
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Return a valid Gemini response
		response := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []map[string]interface{}{
							{"text": "Test response"},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]int{
				"promptTokenCount":     100,
				"candidatesTokenCount": 50,
				"totalTokenCount":      150,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider
	provider := NewToolProvider(
		"test-gemini",
		"gemini-2.0-flash-exp",
		server.URL,
		providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		false,
	)

	// Simulate a conversation with tool use and tool results:
	// 1. User message
	// 2. Assistant (model) message with functionCall
	// 3. Tool result message
	// 4. New user message (current turn)
	messages := []types.Message{
		{Role: "user", Content: "What's the weather?"},
		{
			Role:  "assistant",
			Parts: []types.ContentPart{types.NewTextPart("Let me check the weather for you.")},

			ToolCalls: []types.MessageToolCall{
				{ID: "call_123", Name: "get_weather", Args: json.RawMessage(`{"location":"SF"}`)},
			},
		},
		{
			Role:  "tool",
			Parts: []types.ContentPart{types.NewTextPart(`{"temperature": 72, "condition": "sunny"}`)},

			ToolResult: &types.MessageToolResult{
				ID:    "call_123",
				Name:  "get_weather",
				Parts: []types.ContentPart{types.NewTextPart(`{"temperature": 72, "condition": "sunny"}`)},
			},
		},
		{Role: "user", Content: "Thanks! Now check NYC."},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	// Build tool descriptors
	tools := []providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		},
	}
	geminiTools, _ := provider.BuildTooling([]*providers.ToolDescriptor{&tools[0]})

	// Make the request
	ctx := context.Background()
	_, _, err := provider.PredictWithTools(ctx, req, geminiTools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	// Verify the captured request has correct structure
	if capturedRequest == nil {
		t.Fatal("No request was captured")
	}

	// Extract contents from request
	contents, ok := capturedRequest["contents"].([]interface{})
	if !ok {
		t.Fatalf("Expected contents to be array, got: %T", capturedRequest["contents"])
	}

	// Expected structure:
	// Content 0: user - "What's the weather?"
	// Content 1: model - text + functionCall
	// Content 2: user - functionResponse + text "Thanks! Now check NYC."
	if len(contents) != 3 {
		t.Errorf("Expected 3 contents, got %d", len(contents))
		for i, content := range contents {
			contentMap := content.(map[string]interface{})
			t.Logf("Content %d: role=%s, parts=%v", i, contentMap["role"], contentMap["parts"])
		}
	}

	// Check content 2 (user message with functionResponse)
	content2 := contents[2].(map[string]interface{})
	if content2["role"] != "user" {
		t.Errorf("Content 2 should be 'user', got: %s", content2["role"])
	}

	// Check parts of content 2
	parts2 := content2["parts"].([]interface{})
	if len(parts2) != 2 {
		t.Errorf("Content 2 parts should have 2 items (functionResponse + text), got %d", len(parts2))
		t.Logf("Parts: %+v", parts2)
		t.FailNow()
	}

	// First part should be functionResponse
	part0 := parts2[0].(map[string]interface{})
	if _, hasFunctionResponse := part0["functionResponse"]; !hasFunctionResponse {
		t.Errorf("First part should have 'functionResponse', got: %v", part0)
	}

	// Verify functionResponse structure
	funcResp := part0["functionResponse"].(map[string]interface{})
	if funcResp["name"] != "get_weather" {
		t.Errorf("Expected function name 'get_weather', got: %s", funcResp["name"])
	}

	// Second part should be text
	part1 := parts2[1].(map[string]interface{})
	if part1["text"] != "Thanks! Now check NYC." {
		t.Errorf("Expected text 'Thanks! Now check NYC.', got: %s", part1["text"])
	}

	t.Logf("✅ Tool results properly converted to Gemini functionResponse format")
	t.Logf("Request structure validated successfully")
}

// TestToolProvider_MultipleToolResultsGrouped tests that multiple consecutive
// tool messages are grouped together in a single user message
func TestToolProvider_MultipleToolResultsGrouped(t *testing.T) {
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		response := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []map[string]interface{}{
							{"text": "Test response"},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]int{
				"promptTokenCount":     100,
				"candidatesTokenCount": 50,
				"totalTokenCount":      150,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewToolProvider(
		"test-gemini",
		"gemini-2.0-flash-exp",
		server.URL,
		providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		false,
	)

	// Simulate model making multiple tool calls, followed by multiple tool results
	messages := []types.Message{
		{Role: "user", Content: "Get weather for SF and NYC"},
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "call_sf", Name: "get_weather", Args: json.RawMessage(`{"location":"SF"}`)},
				{ID: "call_nyc", Name: "get_weather", Args: json.RawMessage(`{"location":"NYC"}`)},
			},
		},
		{
			Role:  "tool",
			Parts: []types.ContentPart{types.NewTextPart(`{"temperature": 72, "condition": "sunny"}`)},

			ToolResult: &types.MessageToolResult{
				ID:    "call_sf",
				Name:  "get_weather",
				Parts: []types.ContentPart{types.NewTextPart(`{"temperature": 72, "condition": "sunny"}`)},
			},
		},
		{
			Role:  "tool",
			Parts: []types.ContentPart{types.NewTextPart(`{"temperature": 65, "condition": "cloudy"}`)},

			ToolResult: &types.MessageToolResult{
				ID:    "call_nyc",
				Name:  "get_weather",
				Parts: []types.ContentPart{types.NewTextPart(`{"temperature": 65, "condition": "cloudy"}`)},
			},
		},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	tools := []providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		},
	}
	geminiTools, _ := provider.BuildTooling([]*providers.ToolDescriptor{&tools[0]})

	ctx := context.Background()
	_, _, err := provider.PredictWithTools(ctx, req, geminiTools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	contents, ok := capturedRequest["contents"].([]interface{})
	if !ok {
		t.Fatalf("Expected contents to be array")
	}

	// Expected structure:
	// Content 0: user - "Get weather for SF and NYC"
	// Content 1: model - functionCall (SF) + functionCall (NYC)
	// Content 2: user - functionResponse (SF) + functionResponse (NYC)
	if len(contents) != 3 {
		t.Errorf("Expected 3 contents, got %d", len(contents))
		for i, content := range contents {
			contentMap := content.(map[string]interface{})
			t.Logf("Content %d: role=%s", i, contentMap["role"])
		}
		t.FailNow()
	}

	// Check content 2 has both tool results
	content2 := contents[2].(map[string]interface{})
	parts2 := content2["parts"].([]interface{})

	if len(parts2) != 2 {
		t.Errorf("Content 2 should have 2 functionResponse parts, got %d", len(parts2))
		t.FailNow()
	}

	// Verify both are functionResponses
	for i, part := range parts2 {
		partMap := part.(map[string]interface{})
		if _, hasFunctionResponse := partMap["functionResponse"]; !hasFunctionResponse {
			t.Errorf("Part %d should have 'functionResponse', got: %v", i, partMap)
		}
	}

	t.Logf("✅ Multiple tool results properly grouped in single user content")
}

// TestProcessToolMessage_TextOnly verifies that text-only tool results serialize correctly (regression).
func TestProcessToolMessage_TextOnly(t *testing.T) {
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "call_1",
			Name: "lookup",
			Parts: []types.ContentPart{
				types.NewTextPart(`{"status": "ok", "count": 42}`),
			},
		},
	}

	result := processToolMessage(msg)
	funcResp, ok := result["functionResponse"].(map[string]any)
	if !ok {
		t.Fatalf("Expected functionResponse map, got %T", result["functionResponse"])
	}

	if funcResp["name"] != "lookup" {
		t.Errorf("Expected name 'lookup', got '%v'", funcResp["name"])
	}

	response, ok := funcResp["response"].(map[string]any)
	if !ok {
		t.Fatalf("Expected response to be map, got %T", funcResp["response"])
	}

	// JSON object should be used directly (not wrapped in "result")
	if response["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", response["status"])
	}
	if response["count"] != float64(42) {
		t.Errorf("Expected count 42, got %v", response["count"])
	}

	// Should NOT have inlineData for text-only results
	if _, hasInlineData := response["inlineData"]; hasInlineData {
		t.Error("Text-only tool result should not have inlineData")
	}
}

// TestProcessToolMessage_ImagePart verifies that tool results with image parts
// emit inlineData with the correct mimeType.
func TestProcessToolMessage_ImagePart(t *testing.T) {
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk"
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "call_chart",
			Name: "generate_chart",
			Parts: []types.ContentPart{
				types.NewTextPart("Chart generated successfully"),
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/png",
						Data:     &imageData,
					},
				},
			},
		},
	}

	result := processToolMessage(msg)
	funcResp := result["functionResponse"].(map[string]any)

	if funcResp["name"] != "generate_chart" {
		t.Errorf("Expected name 'generate_chart', got '%v'", funcResp["name"])
	}

	response, ok := funcResp["response"].(map[string]any)
	if !ok {
		t.Fatalf("Expected response to be map, got %T", funcResp["response"])
	}

	// Should have text
	if response["text"] != "Chart generated successfully" {
		t.Errorf("Expected text 'Chart generated successfully', got '%v'", response["text"])
	}

	// Should have inlineData
	inlineData, ok := response["inlineData"].(map[string]any)
	if !ok {
		t.Fatalf("Expected inlineData map, got %T", response["inlineData"])
	}

	if inlineData["mimeType"] != "image/png" {
		t.Errorf("Expected mimeType 'image/png', got '%v'", inlineData["mimeType"])
	}

	if inlineData["data"] != imageData {
		t.Errorf("Expected base64 data to match, got '%v'", inlineData["data"])
	}
}

// TestProcessToolMessage_MixedContent tests tool results with text and media parts.
func TestProcessToolMessage_MixedContent(t *testing.T) {
	audioData := "AAABAAAAAAEAAQBFAAEAAQBF"
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "call_tts",
			Name: "text_to_speech",
			Parts: []types.ContentPart{
				types.NewTextPart("Audio generated"),
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						MIMEType: "audio/wav",
						Data:     &audioData,
					},
				},
			},
		},
	}

	result := processToolMessage(msg)
	funcResp := result["functionResponse"].(map[string]any)
	response := funcResp["response"].(map[string]any)

	// Should have both text and inlineData
	if response["text"] != "Audio generated" {
		t.Errorf("Expected text 'Audio generated', got '%v'", response["text"])
	}

	inlineData := response["inlineData"].(map[string]any)
	if inlineData["mimeType"] != "audio/wav" {
		t.Errorf("Expected mimeType 'audio/wav', got '%v'", inlineData["mimeType"])
	}
}

// TestProcessToolMessage_ImageOnlyNoText tests tool results with only an image and no text.
func TestProcessToolMessage_ImageOnlyNoText(t *testing.T) {
	imageData := "iVBORw0KGgoAAAANSUhEUg"
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "call_screenshot",
			Name: "take_screenshot",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/jpeg",
						Data:     &imageData,
					},
				},
			},
		},
	}

	result := processToolMessage(msg)
	funcResp := result["functionResponse"].(map[string]any)
	response := funcResp["response"].(map[string]any)

	// Should NOT have text key
	if _, hasText := response["text"]; hasText {
		t.Error("Image-only tool result should not have text key")
	}

	// Should have inlineData
	inlineData := response["inlineData"].(map[string]any)
	if inlineData["mimeType"] != "image/jpeg" {
		t.Errorf("Expected mimeType 'image/jpeg', got '%v'", inlineData["mimeType"])
	}
	if inlineData["data"] != imageData {
		t.Error("Expected base64 data to match")
	}
}

// TestBuildTextToolResponse_JSONObject tests that JSON objects are used directly.
func TestBuildTextToolResponse_JSONObject(t *testing.T) {
	response := buildTextToolResponse(`{"key": "value"}`)
	respMap, ok := response.(map[string]any)
	if !ok {
		t.Fatalf("Expected map, got %T", response)
	}
	if respMap["key"] != "value" {
		t.Errorf("Expected key 'value', got '%v'", respMap["key"])
	}
}

// TestBuildTextToolResponse_PlainString tests that plain strings get wrapped.
func TestBuildTextToolResponse_PlainString(t *testing.T) {
	response := buildTextToolResponse("hello world")
	respMap, ok := response.(map[string]any)
	if !ok {
		t.Fatalf("Expected map, got %T", response)
	}
	if respMap["result"] != "hello world" {
		t.Errorf("Expected result 'hello world', got '%v'", respMap["result"])
	}
}

// TestBuildTextToolResponse_Primitive tests that primitives get wrapped.
func TestBuildTextToolResponse_Primitive(t *testing.T) {
	response := buildTextToolResponse("42")
	respMap, ok := response.(map[string]any)
	if !ok {
		t.Fatalf("Expected map, got %T", response)
	}
	if respMap["result"] != float64(42) {
		t.Errorf("Expected result 42, got '%v'", respMap["result"])
	}
}

// TestBuildMultimodalToolResponse tests the multimodal response builder directly.
func TestBuildMultimodalToolResponse(t *testing.T) {
	imgData := "base64data"
	result := &types.MessageToolResult{
		ID:   "call_1",
		Name: "gen_image",
		Parts: []types.ContentPart{
			types.NewTextPart("Image ready"),
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					MIMEType: "image/png",
					Data:     &imgData,
				},
			},
		},
	}

	response := buildMultimodalToolResponse(result)

	if response["text"] != "Image ready" {
		t.Errorf("Expected text 'Image ready', got '%v'", response["text"])
	}

	inlineData, ok := response["inlineData"].(map[string]any)
	if !ok {
		t.Fatalf("Expected inlineData map, got %T", response["inlineData"])
	}
	if inlineData["mimeType"] != "image/png" {
		t.Errorf("Expected mimeType 'image/png', got '%v'", inlineData["mimeType"])
	}
	if inlineData["data"] != imgData {
		t.Error("Expected data to match")
	}
}

// TestProcessToolMessage_UsesToolResultContent verifies that processToolMessage
// uses ToolResult.Content (not msg.Content) for the tool response.
// This is critical for streaming with tools to work correctly.
func TestProcessToolMessage_UsesToolResultContent(t *testing.T) {
	// Create a tool result message as the SDK creates them:
	// - msg.Content is NOT set (empty)
	// - msg.ToolResult.GetTextContent() has the actual data
	toolResultMsg := types.Message{
		Role: "tool",
		// Content is intentionally empty - this is how the SDK creates tool result messages
		ToolResult: &types.MessageToolResult{
			ID:    "call_abc123",
			Name:  "weather",
			Parts: []types.ContentPart{types.NewTextPart(`{"temperature": 73, "conditions": "sunny"}`)},

			Error: "",
		},
	}

	// Process the tool message
	result := processToolMessage(toolResultMsg)

	// Verify the result has functionResponse
	funcResponse, ok := result["functionResponse"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected functionResponse map, got %T", result["functionResponse"])
	}

	// Verify name is set
	if funcResponse["name"] != "weather" {
		t.Errorf("Expected name 'weather', got '%v'", funcResponse["name"])
	}

	// CRITICAL: Verify response contains the tool result data (not empty)
	response, ok := funcResponse["response"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected response to be map, got %T: %v", funcResponse["response"], funcResponse["response"])
	}

	// Check for actual content - must have the temperature from our test data
	// (not just an empty "result" key from the fallback)
	temp, hasTemp := response["temperature"]
	if !hasTemp {
		t.Fatal("Tool result response missing 'temperature' - ToolResult.Content should be used, not msg.Content")
	}
	if temp != float64(73) {
		t.Errorf("Expected temperature 73, got %v", temp)
	}

	conditions, hasCond := response["conditions"]
	if !hasCond {
		t.Fatal("Tool result response missing 'conditions' - ToolResult.Content should be used")
	}
	if conditions != "sunny" {
		t.Errorf("Expected conditions 'sunny', got %v", conditions)
	}
}
