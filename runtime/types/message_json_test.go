package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMessage_JSONMarshal_ToolResultOmitsContent tests that when a message has a ToolResult,
// the Content field is omitted from JSON to avoid duplication
func TestMessage_JSONMarshal_ToolResultOmitsContent(t *testing.T) {
	msg := Message{
		Role:    "tool",
		Content: `{"result": "success"}`,
		ToolResult: &MessageToolResult{
			ID:      "call_123",
			Name:    "test_tool",
			Content: `{"result": "success"}`,
		},
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	// Parse back to verify structure
	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	// Content field should NOT be present in JSON
	_, hasContent := parsed["content"]
	assert.False(t, hasContent, "Content field should be omitted when ToolResult is present")

	// ToolResult should be present
	toolResult, hasToolResult := parsed["tool_result"]
	assert.True(t, hasToolResult, "ToolResult should be present")

	// ToolResult.Content should have the content
	if hasToolResult {
		toolResultMap := toolResult.(map[string]interface{})
		assert.Equal(t, `{"result": "success"}`, toolResultMap["content"])
	}
}

// TestMessage_JSONMarshal_RegularMessageHasContent tests that regular messages
// (without ToolResult) still include the Content field
func TestMessage_JSONMarshal_RegularMessageHasContent(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Hello world",
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	// Content field SHOULD be present
	content, hasContent := parsed["content"]
	assert.True(t, hasContent, "Content field should be present for regular messages")
	assert.Equal(t, "Hello world", content)
}

// TestMessage_JSONUnmarshal_ToolResult tests that unmarshaling still works correctly
func TestMessage_JSONUnmarshal_ToolResult(t *testing.T) {
	jsonData := `{
		"role": "tool",
		"tool_result": {
			"id": "call_123",
			"name": "test_tool",
			"content": "{\"result\": \"success\"}"
		}
	}`

	var msg Message
	err := json.Unmarshal([]byte(jsonData), &msg)
	require.NoError(t, err)

	assert.Equal(t, "tool", msg.Role)
	require.NotNil(t, msg.ToolResult)
	assert.Equal(t, "call_123", msg.ToolResult.ID)
	assert.Equal(t, "test_tool", msg.ToolResult.Name)
	assert.Equal(t, `{"result": "success"}`, msg.ToolResult.Content)

	// After unmarshaling, Content should be set from ToolResult for provider compatibility
	assert.Equal(t, `{"result": "success"}`, msg.Content)
}
