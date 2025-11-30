package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileMockRepository_GetTurn_TextResponse(t *testing.T) {
	// Test backward compatibility with string responses
	configData := `
defaultResponse: "Global default"
scenarios:
  test-scenario:
    turns:
      1: "Simple text response"
      2: "Another text response"
`

	tempFile := createTempYAMLFile(t, configData)
	defer cleanupTempFile(t, tempFile)

	repo, err := NewFileMockRepository(tempFile)
	require.NoError(t, err)

	ctx := context.Background()

	// Test turn 1
	params := ResponseParams{
		ScenarioID: "test-scenario",
		TurnNumber: 1,
	}

	turn, err := repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Simple text response", turn.Content)
	assert.Empty(t, turn.ToolCalls)

	// Test turn 2
	params.TurnNumber = 2
	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Another text response", turn.Content)
	assert.Empty(t, turn.ToolCalls)
}

func TestFileMockRepository_GetTurn_ToolCallResponse(t *testing.T) {
	// Test new structured tool call responses
	configData := `
scenarios:
  tool-scenario:
    turns:
      1:
        type: tool_calls
        content: "I'll help you with that."
        tool_calls:
          - name: get_weather
            arguments:
              location: "San Francisco"
              unit: "celsius"
          - name: send_notification
            arguments:
              message: "Weather updated"
              recipient: "user@example.com"
      2:
        type: text
        content: "Based on the weather data, it's 20°C and cloudy."
`

	tempFile := createTempYAMLFile(t, configData)
	defer cleanupTempFile(t, tempFile)

	repo, err := NewFileMockRepository(tempFile)
	require.NoError(t, err)

	ctx := context.Background()

	// Test tool call turn
	params := ResponseParams{
		ScenarioID: "tool-scenario",
		TurnNumber: 1,
	}

	turn, err := repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "tool_calls", turn.Type)
	assert.Equal(t, "I'll help you with that.", turn.Content)
	assert.Len(t, turn.ToolCalls, 2)

	// Verify first tool call
	assert.Equal(t, "get_weather", turn.ToolCalls[0].Name)
	assert.Equal(t, "San Francisco", turn.ToolCalls[0].Arguments["location"])
	assert.Equal(t, "celsius", turn.ToolCalls[0].Arguments["unit"])

	// Verify second tool call
	assert.Equal(t, "send_notification", turn.ToolCalls[1].Name)
	assert.Equal(t, "Weather updated", turn.ToolCalls[1].Arguments["message"])
	assert.Equal(t, "user@example.com", turn.ToolCalls[1].Arguments["recipient"])

	// Test follow-up text turn
	params.TurnNumber = 2
	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Based on the weather data, it's 20°C and cloudy.", turn.Content)
	assert.Empty(t, turn.ToolCalls)
}

func TestFileMockRepository_GetTurn_MixedResponses(t *testing.T) {
	// Test mix of old string format and new structured format
	configData := `
scenarios:
  mixed-scenario:
    turns:
      1: "Old style string response"
      2:
        type: text
        content: "New style text response"
      3:
        type: tool_calls
        content: "Calling tools now"
        tool_calls:
          - name: example_tool
            arguments:
              param: "value"
`

	tempFile := createTempYAMLFile(t, configData)
	defer cleanupTempFile(t, tempFile)

	repo, err := NewFileMockRepository(tempFile)
	require.NoError(t, err)

	ctx := context.Background()
	params := ResponseParams{
		ScenarioID: "mixed-scenario",
		TurnNumber: 1,
	}

	// Test old string format
	turn, err := repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Old style string response", turn.Content)

	// Test new text format
	params.TurnNumber = 2
	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "New style text response", turn.Content)

	// Test new tool call format
	params.TurnNumber = 3
	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "tool_calls", turn.Type)
	assert.Equal(t, "Calling tools now", turn.Content)
	assert.Len(t, turn.ToolCalls, 1)
	assert.Equal(t, "example_tool", turn.ToolCalls[0].Name)
}

func TestFileMockRepository_GetTurn_DefaultsAndFallbacks(t *testing.T) {
	configData := `
defaultResponse: "Global fallback"
scenarios:
  test-scenario:
    defaultResponse: "Scenario default"
    turns:
      1: "Specific turn response"
`

	tempFile := createTempYAMLFile(t, configData)
	defer cleanupTempFile(t, tempFile)

	repo, err := NewFileMockRepository(tempFile)
	require.NoError(t, err)

	ctx := context.Background()

	// Test specific turn
	params := ResponseParams{
		ScenarioID: "test-scenario",
		TurnNumber: 1,
	}
	turn, err := repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "Specific turn response", turn.Content)

	// Test scenario default (non-existent turn)
	params.TurnNumber = 99
	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "Scenario default", turn.Content)

	// Test global default (non-existent scenario)
	params.ScenarioID = "non-existent"
	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "Global fallback", turn.Content)

	// Test final fallback (no scenario, no defaults)
	params.ScenarioID = ""
	params.ProviderID = "test-provider"
	params.ModelName = "test-model"

	// Create repo without defaults
	emptyConfig := `scenarios: {}`
	tempFile2 := createTempYAMLFile(t, emptyConfig)
	defer cleanupTempFile(t, tempFile2)

	repo2, err := NewFileMockRepository(tempFile2)
	require.NoError(t, err)

	turn, err = repo2.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Contains(t, turn.Content, "test-provider")
	assert.Contains(t, turn.Content, "test-model")
}

func TestFileMockRepository_parseTurnResponse_ErrorHandling(t *testing.T) {
	repo := &FileMockRepository{}

	// Test invalid type
	_, err := repo.parseTurnResponse(123) // Number instead of string or map
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported turn response type")

	// Test string response
	turn, err := repo.parseTurnResponse("Simple string")
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Simple string", turn.Content)

	// Test map without type (should default to "text")
	mapResponse := map[string]interface{}{
		"content": "Map without type",
	}
	turn, err = repo.parseTurnResponse(mapResponse)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Map without type", turn.Content)
}

func TestFileMockRepository_parseToolCalls_ErrorHandling(t *testing.T) {
	repo := &FileMockRepository{}

	// Test invalid tool call structure
	invalidToolCalls := []interface{}{
		"not a map", // Should be a map
	}

	_, err := repo.parseToolCalls(invalidToolCalls)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool call 0 is not a map")

	// Test valid tool calls
	validToolCalls := []interface{}{
		map[string]interface{}{
			"name": "test_tool",
			"arguments": map[string]interface{}{
				"param1": "value1",
				"param2": 42,
			},
		},
	}

	toolCalls, err := repo.parseToolCalls(validToolCalls)
	require.NoError(t, err)
	assert.Len(t, toolCalls, 1)
	assert.Equal(t, "test_tool", toolCalls[0].Name)
	assert.Equal(t, "value1", toolCalls[0].Arguments["param1"])
	assert.Equal(t, 42, toolCalls[0].Arguments["param2"]) // Direct map data preserves original type
}

func TestFileMockRepository_GetTurn_WithToolResponses(t *testing.T) {
	// Deprecated: tool_responses support removed. Keep placeholder to ensure backward compatibility expectations are clear.
	t.Skip("tool_responses support removed from mock repository")
}

func TestInMemoryMockRepository_GetTurn(t *testing.T) {
	repo := NewInMemoryMockRepository("Default response")

	// Test default
	ctx := context.Background()
	params := ResponseParams{}

	turn, err := repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Default response", turn.Content)

	// Add specific responses
	repo.SetResponse("test-scenario", 1, "Turn 1 response")
	repo.SetResponse("test-scenario", 2, "Turn 2 response")

	// Test specific turn
	params.ScenarioID = "test-scenario"
	params.TurnNumber = 1

	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "text", turn.Type)
	assert.Equal(t, "Turn 1 response", turn.Content)

	// Test scenario fallback (turn 0)
	repo.SetResponse("test-scenario", 0, "Scenario default")
	params.TurnNumber = 99 // Non-existent turn

	turn, err = repo.GetTurn(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "Scenario default", turn.Content)
}

func TestFileMockRepository_BackwardCompatibility(t *testing.T) {
	// Ensure GetResponse still works with old code
	configData := `
defaultResponse: "Global default"
scenarios:
  test:
    turns:
      1: "Simple response"
      2:
        type: text
        content: "Structured text response"
      3:
        type: tool_calls
        content: "Tool call response"
        tool_calls:
          - name: test_tool
            arguments: {}
`

	tempFile := createTempYAMLFile(t, configData)
	defer cleanupTempFile(t, tempFile)

	repo, err := NewFileMockRepository(tempFile)
	require.NoError(t, err)

	ctx := context.Background()

	// Test GetResponse method (backward compatibility)
	params := ResponseParams{
		ScenarioID: "test",
		TurnNumber: 1,
	}

	response, err := repo.GetResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "Simple response", response)

	// Test with structured text response
	params.TurnNumber = 2
	response, err = repo.GetResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "Structured text response", response)

	// Test with tool call response (should return content)
	params.TurnNumber = 3
	response, err = repo.GetResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, "Tool call response", response)
}
