package gemini

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestProcessToolMessage(t *testing.T) {
	tests := []struct {
		name          string
		msg           types.Message
		expectedName  string
		checkResponse bool
		responseKey   string
	}{
		{
			name: "Valid JSON object response",
			msg: types.Message{
				Role:    "tool",
				Content: `{"result": "success", "value": 42}`,
				ToolResult: &types.MessageToolResult{
					Name: "get_weather",
					ID:   "call_123",
				},
			},
			expectedName:  "get_weather",
			checkResponse: true,
		},
		{
			name: "Plain text response wrapped in object",
			msg: types.Message{
				Role:    "tool",
				Content: "Just plain text",
				ToolResult: &types.MessageToolResult{
					Name: "calculator",
					ID:   "call_456",
				},
			},
			expectedName:  "calculator",
			checkResponse: true,
			responseKey:   "result",
		},
		{
			name: "JSON primitive wrapped in object",
			msg: types.Message{
				Role:    "tool",
				Content: `42`,
				ToolResult: &types.MessageToolResult{
					Name: "number_generator",
					ID:   "call_789",
				},
			},
			expectedName:  "number_generator",
			checkResponse: true,
			responseKey:   "result",
		},
		{
			name: "Empty name field logs warning",
			msg: types.Message{
				Role:    "tool",
				Content: `{"data": "test"}`,
				ToolResult: &types.MessageToolResult{
					Name: "", // Empty name
					ID:   "call_empty",
				},
			},
			expectedName:  "",
			checkResponse: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processToolMessage(tt.msg)

			// Check structure
			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			funcResp, ok := result["functionResponse"].(map[string]interface{})
			if !ok {
				t.Fatal("Expected functionResponse field")
			}

			// Check name
			if funcResp["name"] != tt.expectedName {
				t.Errorf("Expected name %q, got %q", tt.expectedName, funcResp["name"])
			}

			// Check response exists
			if tt.checkResponse {
				if funcResp["response"] == nil {
					t.Error("Expected response field to exist")
				}

				// For wrapped responses, check the wrapper
				if tt.responseKey != "" {
					respMap, ok := funcResp["response"].(map[string]interface{})
					if !ok {
						t.Error("Expected response to be a map when wrapping primitives")
					} else if respMap[tt.responseKey] == nil {
						t.Errorf("Expected response to have %q key", tt.responseKey)
					}
				}
			}
		})
	}
}

func TestBuildMessageParts(t *testing.T) {
	tests := []struct {
		name               string
		msg                types.Message
		pendingToolResults []map[string]interface{}
		expectedMinParts   int
		checkToolCall      bool
		checkText          bool
		checkToolResults   bool
	}{
		{
			name: "User message with text only",
			msg: types.Message{
				Role:    "user",
				Content: "Hello, how are you?",
			},
			pendingToolResults: nil,
			expectedMinParts:   1,
			checkText:          true,
		},
		{
			name: "User message with pending tool results",
			msg: types.Message{
				Role:    "user",
				Content: "What about now?",
			},
			pendingToolResults: []map[string]interface{}{
				{
					"functionResponse": map[string]interface{}{
						"name":     "get_weather",
						"response": map[string]string{"temp": "72F"},
					},
				},
			},
			expectedMinParts: 2, // Tool result + text
			checkText:        true,
			checkToolResults: true,
		},
		{
			name: "Assistant message with tool calls",
			msg: types.Message{
				Role:    "assistant",
				Content: "Let me check that",
				ToolCalls: []types.MessageToolCall{
					{
						ID:   "call_1",
						Name: "search",
						Args: json.RawMessage(`{"query": "test"}`),
					},
				},
			},
			pendingToolResults: nil,
			expectedMinParts:   2, // Text + tool call
			checkToolCall:      true,
			checkText:          true,
		},
		{
			name: "Assistant message with multiple tool calls",
			msg: types.Message{
				Role:    "assistant",
				Content: "",
				ToolCalls: []types.MessageToolCall{
					{
						ID:   "call_1",
						Name: "tool1",
						Args: json.RawMessage(`{"a": 1}`),
					},
					{
						ID:   "call_2",
						Name: "tool2",
						Args: json.RawMessage(`{"b": 2}`),
					},
				},
			},
			pendingToolResults: nil,
			expectedMinParts:   2,
			checkToolCall:      true,
		},
		{
			name: "Empty message returns empty parts",
			msg: types.Message{
				Role:    "user",
				Content: "",
			},
			pendingToolResults: nil,
			expectedMinParts:   0,
		},
		{
			name: "Non-user message ignores pending tool results",
			msg: types.Message{
				Role:    "assistant",
				Content: "Response",
			},
			pendingToolResults: []map[string]interface{}{
				{"functionResponse": map[string]interface{}{}},
			},
			expectedMinParts: 1,
			checkText:        true,
			checkToolResults: false, // Should not include tool results
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := buildMessageParts(tt.msg, tt.pendingToolResults)

			if len(parts) < tt.expectedMinParts {
				t.Errorf("Expected at least %d parts, got %d", tt.expectedMinParts, len(parts))
			}

			// Check for text content
			if tt.checkText {
				foundText := false
				for _, part := range parts {
					if partMap, ok := part.(map[string]interface{}); ok {
						if text, ok := partMap["text"].(string); ok && text == tt.msg.Content {
							foundText = true
							break
						}
					}
				}
				if !foundText && tt.msg.Content != "" {
					t.Error("Expected to find text content in parts")
				}
			}

			// Check for tool calls
			if tt.checkToolCall {
				foundToolCall := false
				for _, part := range parts {
					if partMap, ok := part.(map[string]interface{}); ok {
						if _, ok := partMap["functionCall"]; ok {
							foundToolCall = true
							break
						}
					}
				}
				if !foundToolCall {
					t.Error("Expected to find functionCall in parts")
				}
			}

			// Check for tool results
			if tt.checkToolResults {
				foundToolResult := false
				for _, part := range parts {
					if partMap, ok := part.(map[string]interface{}); ok {
						if _, ok := partMap["functionResponse"]; ok {
							foundToolResult = true
							break
						}
					}
				}
				if !foundToolResult && len(tt.pendingToolResults) > 0 {
					t.Error("Expected to find tool results in parts")
				}
			}
		})
	}
}

func TestAddToolConfig(t *testing.T) {
	tests := []struct {
		name         string
		toolChoice   string
		expectedMode string
	}{
		{
			name:         "auto maps to AUTO",
			toolChoice:   "auto",
			expectedMode: "AUTO",
		},
		{
			name:         "required maps to ANY",
			toolChoice:   "required",
			expectedMode: "ANY",
		},
		{
			name:         "any maps to ANY",
			toolChoice:   "any",
			expectedMode: "ANY",
		},
		{
			name:         "none maps to NONE",
			toolChoice:   "none",
			expectedMode: "NONE",
		},
		{
			name:         "specific tool name maps to ANY",
			toolChoice:   "get_weather",
			expectedMode: "ANY",
		},
		{
			name:         "empty string defaults to AUTO",
			toolChoice:   "",
			expectedMode: "AUTO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := make(map[string]interface{})
			tools := map[string]interface{}{
				"function_declarations": []interface{}{
					map[string]interface{}{
						"name":        "test_tool",
						"description": "A test tool",
					},
				},
			}

			addToolConfig(request, tools, tt.toolChoice)

			// Check tools were added
			if request["tools"] == nil {
				t.Fatal("Expected tools to be added to request")
			}

			// Check tool_config
			toolConfig, ok := request["tool_config"].(map[string]interface{})
			if !ok {
				t.Fatal("Expected tool_config to be a map")
			}

			funcConfig, ok := toolConfig["function_calling_config"].(map[string]interface{})
			if !ok {
				t.Fatal("Expected function_calling_config to be a map")
			}

			mode, ok := funcConfig["mode"].(string)
			if !ok {
				t.Fatal("Expected mode to be a string")
			}

			if mode != tt.expectedMode {
				t.Errorf("Expected mode %q, got %q", tt.expectedMode, mode)
			}
		})
	}
}

func TestGeminiToolHelpers_Integration(t *testing.T) {
	t.Run("Full tool request flow", func(t *testing.T) {
		// Simulate a conversation with tool calls and results

		// Step 1: User asks a question
		userMsg := types.Message{
			Role:    "user",
			Content: "What's the weather?",
		}
		userParts := buildMessageParts(userMsg, nil)
		if len(userParts) != 1 {
			t.Errorf("Expected 1 part for user message, got %d", len(userParts))
		}

		// Step 2: Assistant makes a tool call
		assistantMsg := types.Message{
			Role:    "assistant",
			Content: "Let me check",
			ToolCalls: []types.MessageToolCall{
				{
					ID:   "call_123",
					Name: "get_weather",
					Args: json.RawMessage(`{"location": "San Francisco"}`),
				},
			},
		}
		assistantParts := buildMessageParts(assistantMsg, nil)
		if len(assistantParts) != 2 {
			t.Errorf("Expected 2 parts (text + tool call), got %d", len(assistantParts))
		}

		// Step 3: Tool returns result
		toolMsg := types.Message{
			Role:    "tool",
			Content: `{"temperature": "72F", "condition": "sunny"}`,
			ToolResult: &types.MessageToolResult{
				Name: "get_weather",
				ID:   "call_123",
			},
		}
		toolResponse := processToolMessage(toolMsg)
		if toolResponse == nil {
			t.Fatal("Expected non-nil tool response")
		}

		// Step 4: User message with pending tool result
		followUpMsg := types.Message{
			Role:    "user",
			Content: "Thanks!",
		}
		pendingResults := []map[string]interface{}{toolResponse}
		followUpParts := buildMessageParts(followUpMsg, pendingResults)
		if len(followUpParts) != 2 {
			t.Errorf("Expected 2 parts (tool result + text), got %d", len(followUpParts))
		}

		// Step 5: Add tool config
		request := make(map[string]interface{})
		tools := map[string]interface{}{"function_declarations": []interface{}{}}
		addToolConfig(request, tools, "auto")

		if request["tool_config"] == nil {
			t.Error("Expected tool_config to be set")
		}
	})
}
