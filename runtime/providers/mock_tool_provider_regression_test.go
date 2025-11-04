package providers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockToolProvider_ToolCallSimulation_Integration tests the complete tool call simulation
// workflow from YAML parsing to tool call generation to prevent regressions.
func TestMockToolProvider_ToolCallSimulation_Integration(t *testing.T) {
	// Create a temporary YAML config file
	configData := `
scenarios:
  test-scenario:
    turns:
      1:
        tool_calls:
          - name: "get_weather"
            arguments:
              location: "San Francisco"
              unit: "celsius"
          - name: "get_time"
            arguments:
              timezone: "America/Los_Angeles"
      2:
        response: "Based on the data I retrieved, it's currently sunny and 72°F in San Francisco."
      3:
        tool_calls:
          - name: "create_reminder"
            arguments:
              text: "Check weather again tomorrow"
              time: "09:00"
      4:
        response: "I've set a reminder for you to check the weather again tomorrow at 9 AM."

default_response: "I don't have information for this scenario."
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")
	err := os.WriteFile(configPath, []byte(configData), 0644)
	require.NoError(t, err)

	// Load the configuration
	repo, err := NewFileMockRepository(configPath)
	require.NoError(t, err)

	// Create MockToolProvider
	provider := NewMockToolProviderWithRepository("test-provider", "test-model", false, repo)

	ctx := context.Background()

	t.Run("Turn 1 - Tool Calls Generated", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "What's the weather like?"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "test-scenario",
				"mock_turn_number": 1,
			},
		}

		response, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")

		require.NoError(t, err)
		assert.Len(t, toolCalls, 2, "Should generate 2 tool calls")

		// Verify first tool call
		assert.Equal(t, "get_weather", toolCalls[0].Name)
		var args1 map[string]interface{}
		err = json.Unmarshal(toolCalls[0].Args, &args1)
		require.NoError(t, err)
		assert.Equal(t, "San Francisco", args1["location"])
		assert.Equal(t, "celsius", args1["unit"])

		// Verify second tool call
		assert.Equal(t, "get_time", toolCalls[1].Name)
		var args2 map[string]interface{}
		err = json.Unmarshal(toolCalls[1].Args, &args2)
		require.NoError(t, err)
		assert.Equal(t, "America/Los_Angeles", args2["timezone"])

		// Response should be empty for tool call turns
		assert.Empty(t, response.Content)
		assert.NotNil(t, response.CostInfo)
	})

	t.Run("Turn 2 - Text Response", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "What's the weather like?"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "test-scenario",
				"mock_turn_number": 2,
			},
		}

		response, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")

		require.NoError(t, err)
		assert.Empty(t, toolCalls, "Should not generate tool calls for text response turn")
		assert.Contains(t, response.Content, "sunny and 72°F")
		assert.NotNil(t, response.CostInfo)
	})

	t.Run("Turn 3 - More Tool Calls", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Set a reminder for me"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "test-scenario",
				"mock_turn_number": 3,
			},
		}

		_, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")

		require.NoError(t, err)
		assert.Len(t, toolCalls, 1, "Should generate 1 tool call")
		assert.Equal(t, "create_reminder", toolCalls[0].Name)

		var args map[string]interface{}
		err = json.Unmarshal(toolCalls[0].Args, &args)
		require.NoError(t, err)
		assert.Equal(t, "Check weather again tomorrow", args["text"])
		assert.Equal(t, "09:00", args["time"])
	})

	t.Run("Turn 4 - Final Text Response", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Set a reminder for me"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "test-scenario",
				"mock_turn_number": 4,
			},
		}

		response, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")

		require.NoError(t, err)
		assert.Empty(t, toolCalls, "Should not generate tool calls for text response turn")
		assert.Contains(t, response.Content, "I've set a reminder")
		assert.Contains(t, response.Content, "9 AM")
	})
}

// TestMockRepository_ToolCallTypeParsing tests that YAML parsing correctly identifies
// tool call turns and sets the type appropriately.
func TestMockRepository_ToolCallTypeParsing(t *testing.T) {
	configData := `
scenarios:
  type-detection-test:
    turns:
      1:
        # Implicit tool calls - should auto-detect type
        tool_calls:
          - name: "test_tool"
            arguments:
              param: "value"
      2:
        # Explicit text response
        response: "This is a text response"
      3:
        # Explicit type override
        type: "custom_type"
        tool_calls:
          - name: "another_tool"
            arguments:
              data: 123
      4:
        # Mixed content - tool calls should take precedence
        response: "This text will be ignored"
        tool_calls:
          - name: "mixed_tool"
            arguments:
              flag: true
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "type-test-config.yaml")
	err := os.WriteFile(configPath, []byte(configData), 0644)
	require.NoError(t, err)

	repo, err := NewFileMockRepository(configPath)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Auto-detect tool_calls type", func(t *testing.T) {
		params := MockResponseParams{
			ScenarioID: "type-detection-test",
			TurnNumber: 1,
		}

		turn, err := repo.GetTurn(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, "tool_calls", turn.Type)
		assert.Len(t, turn.ToolCalls, 1)
		assert.Equal(t, "test_tool", turn.ToolCalls[0].Name)
	})

	t.Run("Explicit text response", func(t *testing.T) {
		params := MockResponseParams{
			ScenarioID: "type-detection-test",
			TurnNumber: 2,
		}

		turn, err := repo.GetTurn(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, "text", turn.Type)
		assert.Equal(t, "This is a text response", turn.Content)
		assert.Empty(t, turn.ToolCalls)
	})

	t.Run("Explicit type override", func(t *testing.T) {
		params := MockResponseParams{
			ScenarioID: "type-detection-test",
			TurnNumber: 3,
		}

		turn, err := repo.GetTurn(ctx, params)
		require.NoError(t, err)
		// Explicit type should be preserved even with tool calls
		assert.Equal(t, "custom_type", turn.Type)
		assert.Len(t, turn.ToolCalls, 1)
		assert.Equal(t, "another_tool", turn.ToolCalls[0].Name)
	})

	t.Run("Mixed content - tool calls detected", func(t *testing.T) {
		params := MockResponseParams{
			ScenarioID: "type-detection-test",
			TurnNumber: 4,
		}

		turn, err := repo.GetTurn(ctx, params)
		require.NoError(t, err)
		// Should auto-detect as tool_calls despite having response field
		assert.Equal(t, "tool_calls", turn.Type)
		assert.Len(t, turn.ToolCalls, 1)
		assert.Equal(t, "mixed_tool", turn.ToolCalls[0].Name)
		// Response text should still be available
		assert.Equal(t, "This text will be ignored", turn.Content)
	})
}

// TestMockToolProvider_ErrorHandling tests error conditions and edge cases
// to ensure robust behavior.
func TestMockToolProvider_ErrorHandling(t *testing.T) {
	configData := `
scenarios:
  error-test:
    turns:
      1:
        tool_calls:
          - name: "valid_tool"
            arguments:
              param: "value"
          # Invalid tool call - missing arguments
          - name: "incomplete_tool"
      2:
        response: "Valid response"

defaultResponse: "Fallback response"
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "error-test-config.yaml")
	err := os.WriteFile(configPath, []byte(configData), 0644)
	require.NoError(t, err)

	repo, err := NewFileMockRepository(configPath)
	require.NoError(t, err)

	provider := NewMockToolProviderWithRepository("test-provider", "test-model", false, repo)
	ctx := context.Background()

	t.Run("Missing scenario - uses fallback", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Test message"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "nonexistent-scenario",
				"mock_turn_number": 1,
			},
		}

		response, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")
		require.NoError(t, err)
		assert.Empty(t, toolCalls)
		assert.Equal(t, "Fallback response", response.Content)
	})

	t.Run("Missing turn - uses fallback", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Test message"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "error-test",
				"mock_turn_number": 999, // Non-existent turn
			},
		}

		response, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")
		require.NoError(t, err)
		assert.Empty(t, toolCalls)
		assert.Equal(t, "Fallback response", response.Content)
	})

	t.Run("Invalid tool call arguments handled gracefully", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Test message"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "error-test",
				"mock_turn_number": 1,
			},
		}

		_, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")
		require.NoError(t, err)
		// Should still return tool calls, even if one has missing arguments
		assert.Len(t, toolCalls, 2)
		assert.Equal(t, "valid_tool", toolCalls[0].Name)
		assert.Equal(t, "incomplete_tool", toolCalls[1].Name)

		// Check that the incomplete tool call has empty arguments
		var incompleteArgs map[string]interface{}
		err = json.Unmarshal(toolCalls[1].Args, &incompleteArgs)
		require.NoError(t, err)
		assert.Empty(t, incompleteArgs)
	})
}

// TestMockToolProvider_BackwardCompatibility ensures that existing string-based
// mock responses still work alongside new tool call functionality.
func TestMockToolProvider_BackwardCompatibility(t *testing.T) {
	configData := `
# Old-style string responses
scenarios:
  old-style:
    turns:
      1: "Simple string response for turn 1"
      2: "Simple string response for turn 2"

# Mixed old and new style
  mixed-style:
    turns:
      1: "Old style string"
      2:
        response: "New style response"
      3:
        tool_calls:
          - name: "new_tool"
            arguments:
              data: "value"

defaultResponse: "Default fallback"
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "compat-test-config.yaml")
	err := os.WriteFile(configPath, []byte(configData), 0644)
	require.NoError(t, err)

	repo, err := NewFileMockRepository(configPath)
	require.NoError(t, err)

	provider := NewMockToolProviderWithRepository("test-provider", "test-model", false, repo)
	ctx := context.Background()

	t.Run("Old-style string responses work", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Test"},
			},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "old-style",
				"mock_turn_number": 1,
			},
		}

		response, toolCalls, err := provider.ChatWithTools(ctx, req, nil, "auto")
		require.NoError(t, err)
		assert.Empty(t, toolCalls)
		assert.Equal(t, "Simple string response for turn 1", response.Content)
	})

	t.Run("Mixed style scenario works", func(t *testing.T) {
		// Test old-style string in mixed scenario
		req1 := ChatRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "mixed-style",
				"mock_turn_number": 1,
			},
		}

		response1, toolCalls1, err := provider.ChatWithTools(ctx, req1, nil, "auto")
		require.NoError(t, err)
		assert.Empty(t, toolCalls1)
		assert.Equal(t, "Old style string", response1.Content)

		// Test new-style response in mixed scenario
		req2 := ChatRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "mixed-style",
				"mock_turn_number": 2,
			},
		}

		response2, toolCalls2, err := provider.ChatWithTools(ctx, req2, nil, "auto")
		require.NoError(t, err)
		assert.Empty(t, toolCalls2)
		assert.Equal(t, "New style response", response2.Content)

		// Test tool calls in mixed scenario
		req3 := ChatRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "mixed-style",
				"mock_turn_number": 3,
			},
		}

		_, toolCalls3, err := provider.ChatWithTools(ctx, req3, nil, "auto")
		require.NoError(t, err)
		assert.Len(t, toolCalls3, 1)
		assert.Equal(t, "new_tool", toolCalls3[0].Name)
	})
}
