package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestGeminiToolProvider_BuildRequestWithToolMessages tests that tool messages
// from conversation history are properly converted to Gemini's functionResponse format
func TestGeminiToolProvider_BuildRequestWithToolMessages(t *testing.T) {
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
	provider := NewGeminiToolProvider(
		"test-gemini",
		"gemini-2.0-flash-exp",
		server.URL,
		ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
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
			Role:    "assistant",
			Content: "Let me check the weather for you.",
			ToolCalls: []types.MessageToolCall{
				{ID: "call_123", Name: "get_weather", Args: json.RawMessage(`{"location":"SF"}`)},
			},
		},
		{
			Role:    "tool",
			Content: `{"temperature": 72, "condition": "sunny"}`,
			ToolResult: &types.MessageToolResult{
				ID:      "call_123",
				Name:    "get_weather",
				Content: `{"temperature": 72, "condition": "sunny"}`,
			},
		},
		{Role: "user", Content: "Thanks! Now check NYC."},
	}

	req := ChatRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	// Build tool descriptors
	tools := []ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		},
	}
	geminiTools, _ := provider.BuildTooling([]*ToolDescriptor{&tools[0]})

	// Make the request
	ctx := context.Background()
	_, _, err := provider.ChatWithTools(ctx, req, geminiTools, "auto")
	if err != nil {
		t.Fatalf("ChatWithTools failed: %v", err)
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

// TestGeminiToolProvider_MultipleToolResultsGrouped tests that multiple consecutive
// tool messages are grouped together in a single user message
func TestGeminiToolProvider_MultipleToolResultsGrouped(t *testing.T) {
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

	provider := NewGeminiToolProvider(
		"test-gemini",
		"gemini-2.0-flash-exp",
		server.URL,
		ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
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
			Role:    "tool",
			Content: `{"temperature": 72, "condition": "sunny"}`,
			ToolResult: &types.MessageToolResult{
				ID:      "call_sf",
				Name:    "get_weather",
				Content: `{"temperature": 72, "condition": "sunny"}`,
			},
		},
		{
			Role:    "tool",
			Content: `{"temperature": 65, "condition": "cloudy"}`,
			ToolResult: &types.MessageToolResult{
				ID:      "call_nyc",
				Name:    "get_weather",
				Content: `{"temperature": 65, "condition": "cloudy"}`,
			},
		},
	}

	req := ChatRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	tools := []ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		},
	}
	geminiTools, _ := provider.BuildTooling([]*ToolDescriptor{&tools[0]})

	ctx := context.Background()
	_, _, err := provider.ChatWithTools(ctx, req, geminiTools, "auto")
	if err != nil {
		t.Fatalf("ChatWithTools failed: %v", err)
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
