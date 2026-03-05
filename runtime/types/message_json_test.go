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
	result := NewTextToolResult("call_123", "test_tool", `{"result": "success"}`)
	msg := Message{
		Role:       "tool",
		Content:    `{"result": "success"}`,
		ToolResult: &result,
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

	// ToolResult.Parts should have the content
	if hasToolResult {
		toolResultMap := toolResult.(map[string]interface{})
		parts, hasParts := toolResultMap["parts"]
		assert.True(t, hasParts, "ToolResult should have parts")
		partsArr := parts.([]interface{})
		assert.Len(t, partsArr, 1)
		partMap := partsArr[0].(map[string]interface{})
		assert.Equal(t, `{"result": "success"}`, partMap["text"])
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
			"parts": [{"type": "text", "text": "{\"result\": \"success\"}"}]
		}
	}`

	var msg Message
	err := json.Unmarshal([]byte(jsonData), &msg)
	require.NoError(t, err)

	assert.Equal(t, "tool", msg.Role)
	require.NotNil(t, msg.ToolResult)
	assert.Equal(t, "call_123", msg.ToolResult.ID)
	assert.Equal(t, "test_tool", msg.ToolResult.Name)
	assert.Equal(t, `{"result": "success"}`, msg.ToolResult.GetTextContent())

	// After unmarshaling, Content should be set from ToolResult.GetTextContent() for provider compatibility
	assert.Equal(t, `{"result": "success"}`, msg.Content)
}
