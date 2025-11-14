package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestClaudeToolProvider_BuildRequestWithToolMessages tests that tool messages
// from conversation history are properly converted to Claude's tool_result format
func TestClaudeToolProvider_BuildRequestWithToolMessages(t *testing.T) {
	// Track what gets sent to Claude API
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Return a valid Claude response
		response := map[string]interface{}{
			"id":          "msg_test123",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]interface{}{{"type": "text", "text": "Test response"}},
			"model":       "claude-3-5-haiku-20241022",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 100, "output_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider
	provider := NewClaudeToolProvider(
		"test-haiku",
		"claude-3-5-haiku-20241022",
		server.URL,
		providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		false,
	)

	// Simulate a conversation with tool use and tool results:
	// 1. User message
	// 2. Assistant message with tool_use
	// 3. Tool result message
	// 4. New user message (current turn)
	messages := []types.Message{
		{Role: "user", Content: "What's the weather?"},
		{
			Role:    "assistant",
			Content: "Let me check the weather for you.",
			ToolCalls: []types.MessageToolCall{
				{ID: "toolu_123", Name: "get_weather", Args: json.RawMessage(`{"location":"SF"}`)},
			},
		},
		{
			Role:    "tool",
			Content: `{"temperature": 72, "condition": "sunny"}`,
			ToolResult: &types.MessageToolResult{
				ID:      "toolu_123",
				Name:    "get_weather",
				Content: `{"temperature": 72, "condition": "sunny"}`,
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
	claudeTools, _ := provider.BuildTooling([]*providers.ToolDescriptor{&tools[0]})

	// Make the request
	ctx := context.Background()
	_, _, err := provider.PredictWithTools(ctx, req, claudeTools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	// Verify the captured request has correct structure
	if capturedRequest == nil {
		t.Fatal("No request was captured")
	}

	// Extract messages from request
	requestMessages, ok := capturedRequest["messages"].([]interface{})
	if !ok {
		t.Fatalf("Expected messages to be array, got: %T", capturedRequest["messages"])
	}

	// Expected structure:
	// Message 0: user - "What's the weather?"
	// Message 1: assistant - text + tool_use
	// Message 2: user - tool_result + text "Thanks! Now check NYC."
	if len(requestMessages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(requestMessages))
		for i, msg := range requestMessages {
			msgMap := msg.(map[string]interface{})
			t.Logf("Message %d: role=%s, content=%v", i, msgMap["role"], msgMap["content"])
		}
	}

	// Check message 2 (user message with tool_result)
	msg2 := requestMessages[2].(map[string]interface{})
	if msg2["role"] != "user" {
		t.Errorf("Message 2 should be 'user', got: %s", msg2["role"])
	}

	// Check content of message 2
	content2 := msg2["content"].([]interface{})
	if len(content2) != 2 {
		t.Errorf("Message 2 content should have 2 items (tool_result + text), got %d", len(content2))
		t.Logf("Content: %+v", content2)
		t.FailNow()
	}

	// First item should be tool_result
	item0 := content2[0].(map[string]interface{})
	if item0["type"] != "tool_result" {
		t.Errorf("First content item should be 'tool_result', got: %s", item0["type"])
	}
	if item0["tool_use_id"] != "toolu_123" {
		t.Errorf("Expected tool_use_id 'toolu_123', got: %s", item0["tool_use_id"])
	}

	// Second item should be text
	item1 := content2[1].(map[string]interface{})
	if item1["type"] != "text" {
		t.Errorf("Second content item should be 'text', got: %s", item1["type"])
	}
	if item1["text"] != "Thanks! Now check NYC." {
		t.Errorf("Expected text 'Thanks! Now check NYC.', got: %s", item1["text"])
	}

	t.Logf("✅ Tool results properly converted to Claude format")
	t.Logf("Request structure validated successfully")
}

// TestClaudeToolProvider_MultipleToolResultsGrouped tests that multiple consecutive
// tool messages are grouped together in a single user message
func TestClaudeToolProvider_MultipleToolResultsGrouped(t *testing.T) {
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		response := map[string]interface{}{
			"id":          "msg_test123",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]interface{}{{"type": "text", "text": "Test response"}},
			"model":       "claude-3-5-haiku-20241022",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 100, "output_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewClaudeToolProvider(
		"test-haiku",
		"claude-3-5-haiku-20241022",
		server.URL,
		providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		false,
	)

	// Simulate assistant making multiple tool calls, followed by multiple tool results
	messages := []types.Message{
		{Role: "user", Content: "Get weather for SF and NYC"},
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "toolu_sf", Name: "get_weather", Args: json.RawMessage(`{"location":"SF"}`)},
				{ID: "toolu_nyc", Name: "get_weather", Args: json.RawMessage(`{"location":"NYC"}`)},
			},
		},
		{
			Role:    "tool",
			Content: `{"temperature": 72, "condition": "sunny"}`,
			ToolResult: &types.MessageToolResult{
				ID:      "toolu_sf",
				Name:    "get_weather",
				Content: `{"temperature": 72, "condition": "sunny"}`,
			},
		},
		{
			Role:    "tool",
			Content: `{"temperature": 65, "condition": "cloudy"}`,
			ToolResult: &types.MessageToolResult{
				ID:      "toolu_nyc",
				Name:    "get_weather",
				Content: `{"temperature": 65, "condition": "cloudy"}`,
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
	claudeTools, _ := provider.BuildTooling([]*providers.ToolDescriptor{&tools[0]})

	ctx := context.Background()
	_, _, err := provider.PredictWithTools(ctx, req, claudeTools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	requestMessages, ok := capturedRequest["messages"].([]interface{})
	if !ok {
		t.Fatalf("Expected messages to be array")
	}

	// Expected structure:
	// Message 0: user - "Get weather for SF and NYC"
	// Message 1: assistant - tool_use (SF) + tool_use (NYC)
	// Message 2: user - tool_result (SF) + tool_result (NYC)
	if len(requestMessages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(requestMessages))
		for i, msg := range requestMessages {
			msgMap := msg.(map[string]interface{})
			t.Logf("Message %d: role=%s", i, msgMap["role"])
		}
		t.FailNow()
	}

	// Check message 2 has both tool results
	msg2 := requestMessages[2].(map[string]interface{})
	content2 := msg2["content"].([]interface{})

	if len(content2) != 2 {
		t.Errorf("Message 2 should have 2 tool_result items, got %d", len(content2))
		t.FailNow()
	}

	// Verify both are tool_results
	for i, item := range content2 {
		itemMap := item.(map[string]interface{})
		if itemMap["type"] != "tool_result" {
			t.Errorf("Content item %d should be 'tool_result', got: %s", i, itemMap["type"])
		}
	}

	t.Logf("✅ Multiple tool results properly grouped in single user message")
}
