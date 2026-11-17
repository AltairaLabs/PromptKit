package gemini

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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
			result := mustProcessToolMessage(t, tt.msg)

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
		name             string
		msg              types.Message
		expectedMinParts int
		checkToolCall    bool
		checkText        bool
	}{
		{
			name: "User message with text only",
			msg: types.Message{
				Role:    "user",
				Content: "Hello, how are you?",
			},
			expectedMinParts: 1,
			checkText:        true,
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
			expectedMinParts: 2, // Text + tool call
			checkToolCall:    true,
			checkText:        true,
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
			expectedMinParts: 2,
			checkToolCall:    true,
		},
		{
			name: "Empty message returns empty parts",
			msg: types.Message{
				Role:    "user",
				Content: "",
			},
			expectedMinParts: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := mustBuildMessageParts(t, tt.msg)

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
		userParts := mustBuildMessageParts(t, userMsg)
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
		assistantParts := mustBuildMessageParts(t, assistantMsg)
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
		toolResponse := mustProcessToolMessage(t, toolMsg)
		if toolResponse == nil {
			t.Fatal("Expected non-nil tool response")
		}

		// Step 4: The tool result takes a user turn of its own, ahead of the
		// follow-up user message.
		followUpMsg := types.Message{
			Role:    "user",
			Content: "Thanks!",
		}
		contents, err := convProvider().buildToolContents(context.Background(),
			[]types.Message{userMsg, assistantMsg, toolMsg, followUpMsg})
		if err != nil {
			t.Fatalf("buildToolContents: %v", err)
		}
		if len(contents) != 4 {
			t.Fatalf("Expected 4 turns (user, model, tool result, user), got %d", len(contents))
		}
		if got := contents[2][wireKeyParts]; !reflect.DeepEqual(got, []map[string]any{toolResponse}) {
			t.Errorf("tool result turn parts = %v, want %v", got, []map[string]any{toolResponse})
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

// TestBuildMessageParts_ThoughtSignatureRoundTrip verifies that Gemini 3's
// opaque thoughtSignature, captured on an inbound tool-call part, is replayed
// verbatim on the same part when the conversation history is serialized back
// to the Gemini API. Without this, Gemini 3 rejects follow-up requests with:
//
//	"Function call is missing a thought_signature in functionCall parts".
func TestBuildMessageParts_ThoughtSignatureRoundTrip(t *testing.T) {
	const sig = "CpQCAdHtim//eFEosgdaIKSUFynLIA1Y3O+5yKnnzRmeUlMvFlCAB7lBGHVf8"
	msg := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_1",
				Name: "get_weather",
				Args: json.RawMessage(`{"location": "NYC"}`),
				ProviderMetadata: map[string]string{
					"gemini.thought_signature": sig,
				},
			},
		},
	}

	parts := mustBuildMessageParts(t, msg)
	if len(parts) == 0 {
		t.Fatal("expected at least one part")
	}

	// Find the part that carries the functionCall
	var toolPart map[string]any
	for _, p := range parts {
		if pm, ok := p.(map[string]any); ok {
			if _, hasFC := pm["functionCall"]; hasFC {
				toolPart = pm
				break
			}
		}
	}
	if toolPart == nil {
		t.Fatalf("no functionCall part in output: %+v", parts)
	}

	gotSig, _ := toolPart["thoughtSignature"].(string)
	if gotSig != sig {
		t.Errorf("thoughtSignature = %q, want %q", gotSig, sig)
	}

	// Sanity: functionCall content is still intact.
	fc, ok := toolPart["functionCall"].(map[string]any)
	if !ok {
		t.Fatalf("functionCall wrong shape: %T", toolPart["functionCall"])
	}
	if fc["name"] != "get_weather" {
		t.Errorf("functionCall.name = %v, want get_weather", fc["name"])
	}
}

// TestBuildMessageParts_NoThoughtSignature verifies that when a tool call has
// no ProviderMetadata (e.g. from a non-Gemini provider or pre-upgrade history),
// buildMessageParts does not emit an empty thoughtSignature field.
func TestBuildMessageParts_NoThoughtSignature(t *testing.T) {
	msg := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_1",
				Name: "get_weather",
				Args: json.RawMessage(`{"location": "NYC"}`),
			},
		},
	}

	parts := mustBuildMessageParts(t, msg)
	for _, p := range parts {
		if pm, ok := p.(map[string]any); ok {
			if _, hasFC := pm["functionCall"]; hasFC {
				if _, hasSig := pm["thoughtSignature"]; hasSig {
					t.Errorf("unexpected thoughtSignature emitted when metadata is absent: %+v", pm)
				}
				return
			}
		}
	}
	t.Fatal("no functionCall part in output")
}

// TestParseToolResponse_CapturesThoughtSignature verifies the non-streaming
// inbound parse stashes Gemini 3's thoughtSignature in MessageToolCall.ProviderMetadata.
func TestParseToolResponse_CapturesThoughtSignature(t *testing.T) {
	const sig = "CpQCAdHtim//eFEosgdaIKSUFynLIA1Y3O+5yKnnzRmeUlMvFlCAB7lBGHVf8"
	respJSON := `{
  "candidates": [{
    "content": {
      "role": "model",
      "parts": [{
        "functionCall": {"name": "get_weather", "args": {"location": "NYC"}},
        "thoughtSignature": "` + sig + `"
      }]
    },
    "finishReason": "STOP",
    "index": 0
  }]
}`

	provider := NewProvider("test", "gemini-3-flash-preview", "", providers.ProviderDefaults{}, false)
	toolProvider := &ToolProvider{Provider: provider}

	_, toolCalls, err := toolProvider.parseToolResponse([]byte(respJSON), providers.PredictionResponse{})
	if err != nil {
		t.Fatalf("parseToolResponse failed: %v", err)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	gotSig := toolCalls[0].ProviderMetadata["gemini.thought_signature"]
	if gotSig != sig {
		t.Errorf("captured thoughtSignature = %q, want %q", gotSig, sig)
	}
}
