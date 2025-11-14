package gemini

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestGeminiToolProvider_ToolChoiceMapping verifies that tool_choice values
// are correctly mapped to Gemini's function_calling_config modes.
// This is critical to prevent infinite tool loops.
func TestGeminiToolProvider_ToolChoiceMapping(t *testing.T) {
	tests := []struct {
		name         string
		toolChoice   string
		expectedMode string
		description  string
	}{
		{
			name:         "auto should map to AUTO mode",
			toolChoice:   "auto",
			expectedMode: "AUTO",
			description:  "AUTO mode lets Gemini decide when to use tools or return text",
		},
		{
			name:         "required should map to ANY mode",
			toolChoice:   "required",
			expectedMode: "ANY",
			description:  "ANY mode forces tool usage",
		},
		{
			name:         "any should map to ANY mode",
			toolChoice:   "any",
			expectedMode: "ANY",
			description:  "ANY mode forces tool usage",
		},
		{
			name:         "none should map to NONE mode",
			toolChoice:   "none",
			expectedMode: "NONE",
			description:  "NONE mode disables tools",
		},
		{
			name:         "empty should default to AUTO mode",
			toolChoice:   "",
			expectedMode: "AUTO",
			description:  "When not specified, should use AUTO to allow model flexibility",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGeminiToolProvider(
				"test-gemini",
				"gemini-2.0-flash-exp",
				"https://test.example.com",
				providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
				false,
			)

			// Create test tools in Gemini format
			toolDesc := &providers.ToolDescriptor{
				Name:        "test_tool",
				Description: "A test tool",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
			}
			geminiTools, err := provider.BuildTooling([]*providers.ToolDescriptor{toolDesc})
			if err != nil {
				t.Fatalf("Failed to build tooling: %v", err)
			}

			// Build request with tool_choice
			messages := []types.Message{
				{
					Role:    "user",
					Content: "test message",
				},
			}

			req := providers.PredictionRequest{
				Messages:    messages,
				MaxTokens:   1024,
				Temperature: 0.7,
			}

			requestMap := provider.buildToolRequest(req, geminiTools, tt.toolChoice)

			// Extract the mode from tool_config
			toolConfig, ok := requestMap["tool_config"].(map[string]interface{})
			if !ok {
				t.Fatalf("tool_config not found or not a map")
			}

			functionConfig, ok := toolConfig["function_calling_config"].(map[string]interface{})
			if !ok {
				t.Fatalf("function_calling_config not found or not a map")
			}

			actualMode, ok := functionConfig["mode"].(string)
			if !ok {
				t.Fatalf("mode not found or not a string")
			}

			if actualMode != tt.expectedMode {
				t.Errorf("tool_choice %q mapped to mode %q, expected %q\nReason: %s",
					tt.toolChoice, actualMode, tt.expectedMode, tt.description)
			}

			t.Logf("✓ tool_choice=%q correctly mapped to mode=%q (%s)",
				tt.toolChoice, actualMode, tt.description)
		})
	}
}
