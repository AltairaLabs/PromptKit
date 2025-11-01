package validators

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestToolsCalledValidator_WithMessages tests the new approach where validators
// receive messages instead of pre-extracted tool calls
func TestToolsCalledValidator_WithMessages(t *testing.T) {
	tests := []struct {
		name          string
		expectedTools []string
		messages      []types.Message
		wantOK        bool
		wantMissing   []string
	}{
		{
			name:          "Single round with tool calls",
			expectedTools: []string{"search", "calculate"},
			messages: []types.Message{
				{
					Role:      "user",
					Content:   "Search for AI and calculate 15 * 3",
					Timestamp: time.Now(),
				},
				{
					Role:    "assistant",
					Content: "I'll help you with that.",
					ToolCalls: []types.MessageToolCall{
						{Name: "search", ID: "call_1"},
						{Name: "calculate", ID: "call_2"},
					},
					Timestamp: time.Now(),
				},
				{
					Role:    "tool",
					Content: "Search results...",
					ToolResult: &types.MessageToolResult{
						ID:      "call_1",
						Name:    "search",
						Content: "Search results...",
					},
					Timestamp: time.Now(),
				},
				{
					Role:    "tool",
					Content: "45",
					ToolResult: &types.MessageToolResult{
						ID:      "call_2",
						Name:    "calculate",
						Content: "45",
					},
					Timestamp: time.Now(),
				},
				{
					Role:      "assistant",
					Content:   "The search returned results and 15 * 3 = 45",
					Timestamp: time.Now(),
				},
			},
			wantOK:      true,
			wantMissing: nil,
		},
		{
			name:          "Multi-turn conversation - only check current turn",
			expectedTools: []string{}, // Turn 2 expects no tools
			messages: []types.Message{
				// Turn 1
				{Role: "user", Content: "Search for AI", Timestamp: time.Now()},
				{
					Role:    "assistant",
					Content: "Searching...",
					ToolCalls: []types.MessageToolCall{
						{Name: "search", ID: "call_1"},
					},
					Timestamp: time.Now(),
				},
				{Role: "tool", Content: "Results", Timestamp: time.Now()},
				{Role: "assistant", Content: "Here are the results", Timestamp: time.Now()},
				// Turn 2 (current turn being validated)
				{Role: "user", Content: "What's 2 + 2?", Timestamp: time.Now()},
				{Role: "assistant", Content: "4", Timestamp: time.Now()}, // No tools
			},
			wantOK:      true,
			wantMissing: nil,
		},
		{
			name:          "Missing expected tool in current turn",
			expectedTools: []string{"search", "calculate"},
			messages: []types.Message{
				{Role: "user", Content: "Search for AI", Timestamp: time.Now()},
				{
					Role:    "assistant",
					Content: "Searching...",
					ToolCalls: []types.MessageToolCall{
						{Name: "search", ID: "call_1"},
						// Missing "calculate"
					},
					Timestamp: time.Now(),
				},
				{Role: "tool", Content: "Results", Timestamp: time.Now()},
				{Role: "assistant", Content: "Here are the results", Timestamp: time.Now()},
			},
			wantOK:      false,
			wantMissing: []string{"calculate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create validator with factory pattern
			params := map[string]interface{}{
				"tools": tt.expectedTools,
			}
			validator := NewToolsCalledValidator(params)

			// Prepare enhanced params with messages (deep clone)
			clonedMessages := deepCloneMessages(tt.messages)
			enhancedParams := map[string]interface{}{
				"_turn_messages": clonedMessages,
			}

			// Validate
			result := validator.Validate("", enhancedParams)

			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v", result.OK, tt.wantOK)
			}

			// Check missing tools
			if !result.OK {
				details, ok := result.Details.(map[string]interface{})
				if !ok {
					t.Fatalf("Expected details to be map[string]interface{}, got %T", result.Details)
				}

				missing, ok := details["missing_tools"].([]string)
				if !ok {
					t.Fatalf("Expected missing_tools to be []string, got %T", details["missing_tools"])
				}

				if len(missing) != len(tt.wantMissing) {
					t.Errorf("Got %d missing tools, want %d: %v", len(missing), len(tt.wantMissing), missing)
				}

				for i, want := range tt.wantMissing {
					if i >= len(missing) || missing[i] != want {
						t.Errorf("Missing tool %d = %v, want %v", i, missing, tt.wantMissing)
					}
				}
			}
		})
	}
}

// TestToolsCalledValidator_SourceFieldFiltering verifies that tool calls from
// historical messages (Source="statestore") are excluded from validation
func TestToolsCalledValidator_SourceFieldFiltering(t *testing.T) {
	validator := NewToolsCalledValidator(map[string]interface{}{
		"tools": []string{"calculate"},
	})

	// Create messages with both historical and current turn tool calls
	messages := []types.Message{
		// Turn 1 (historical - from StateStore)
		{
			Role:    "user",
			Content: "Calculate 10 + 5",
			Source:  "statestore",
		},
		{
			Role:    "assistant",
			Content: "Calculating...",
			Source:  "statestore",
			ToolCalls: []types.MessageToolCall{
				{Name: "calculate", ID: "call-1"}, // Historical tool call
			},
		},
		{Role: "tool", Content: "15", Source: "statestore"},
		{Role: "assistant", Content: "The result is 15", Source: "statestore"},
		// Turn 2 (current turn - new messages)
		{
			Role:    "user",
			Content: "Now calculate 20 + 5",
			Source:  "", // User input
		},
		{
			Role:    "assistant",
			Content: "Calculating...",
			Source:  "pipeline",
			ToolCalls: []types.MessageToolCall{
				{Name: "calculate", ID: "call-2"}, // Current turn tool call
			},
		},
	}

	enhancedParams := map[string]interface{}{
		"_turn_messages": messages,
	}

	// Should pass because calculate tool was called in current turn
	result := validator.Validate("", enhancedParams)
	if !result.OK {
		t.Errorf("Expected validation to pass (calculate called in current turn), got: %v", result.Details)
	}

	// Test with a tool that was only called in historical turn
	validator2 := NewToolsCalledValidator(map[string]interface{}{
		"tools": []string{"search"},
	})

	messages2 := []types.Message{
		// Historical turn with search
		{
			Role:   "assistant",
			Source: "statestore",
			ToolCalls: []types.MessageToolCall{
				{Name: "search", ID: "call-1"},
			},
		},
		// Current turn without search
		{
			Role:    "user",
			Content: "Calculate something",
			Source:  "",
		},
		{
			Role:    "assistant",
			Content: "Result",
			Source:  "pipeline",
			ToolCalls: []types.MessageToolCall{
				{Name: "calculate", ID: "call-2"},
			},
		},
	}

	enhancedParams2 := map[string]interface{}{
		"_turn_messages": messages2,
	}

	// Should fail because search was only in historical messages
	result2 := validator2.Validate("", enhancedParams2)
	if result2.OK {
		t.Error("Expected validation to fail (search not in current turn)")
	}

	details := result2.Details.(map[string]interface{})
	missing := details["missing_tools"].([]string)
	if len(missing) != 1 || missing[0] != "search" {
		t.Errorf("Expected missing_tools=['search'], got %v", missing)
	}
}
