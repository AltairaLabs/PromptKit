package types

import (
	"encoding/json"
	"testing"
)

// TestMessage_Integration_ToolResultHandling verifies the complete lifecycle:
// 1. Create tool result message with Content field
// 2. Marshal to JSON (should omit Content)
// 3. Unmarshal from JSON (should restore Content from ToolResult)
func TestMessage_Integration_ToolResultHandling(t *testing.T) {
	// Step 1: Create a tool result message with Content set
	originalMsg := &Message{
		Role:    "tool",
		Content: "The weather is sunny",
		ToolResult: &MessageToolResult{
			ID:      "call_123",
			Name:    "get_weather",
			Content: "The weather is sunny",
			Error:   "",
		},
	}

	// Verify Content is set initially
	if originalMsg.Content == "" {
		t.Fatal("Content should be set initially")
	}

	// Step 2: Marshal to JSON
	jsonData, err := json.Marshal(originalMsg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify Content field is NOT in JSON (to avoid duplication)
	var rawJSON map[string]interface{}
	if err := json.Unmarshal(jsonData, &rawJSON); err != nil {
		t.Fatalf("Failed to unmarshal to raw JSON: %v", err)
	}

	if _, hasContent := rawJSON["content"]; hasContent {
		t.Errorf("JSON should NOT contain 'content' field when ToolResult is present\nJSON: %s", string(jsonData))
	}

	// Verify tool_result.content is present
	toolResult, ok := rawJSON["tool_result"].(map[string]interface{})
	if !ok {
		t.Fatal("tool_result field missing or wrong type")
	}
	if toolResult["content"] != "The weather is sunny" {
		t.Errorf("tool_result.content = %v, want 'The weather is sunny'", toolResult["content"])
	}

	// Step 3: Unmarshal back from JSON
	var restoredMsg Message
	if err := json.Unmarshal(jsonData, &restoredMsg); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify Content field is restored for provider compatibility
	if restoredMsg.Content != "The weather is sunny" {
		t.Errorf("Content should be restored from ToolResult.Content\nGot: %q\nWant: %q",
			restoredMsg.Content, "The weather is sunny")
	}

	// Verify ToolResult is intact
	if restoredMsg.ToolResult == nil {
		t.Fatal("ToolResult should not be nil")
	}
	if restoredMsg.ToolResult.Content != "The weather is sunny" {
		t.Errorf("ToolResult.Content = %q, want 'The weather is sunny'", restoredMsg.ToolResult.Content)
	}
}
