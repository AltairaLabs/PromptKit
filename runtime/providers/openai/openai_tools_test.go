package openai

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// ============================================================================
// NewOpenAIToolProvider Tests
// ============================================================================

func TestNewOpenAIToolProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	provider := NewOpenAIToolProvider("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false, nil)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.OpenAIProvider == nil {
		t.Fatal("Expected non-nil OpenAIProvider")
	}

	if provider.ID() != "test-openai" {
		t.Errorf("Expected id 'test-openai', got '%s'", provider.ID())
	}
}

// ============================================================================
// BuildTooling Tests
// ============================================================================

func TestOpenAIBuildTooling_Empty(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	tools, err := provider.BuildTooling(nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if tools != nil {
		t.Errorf("Expected nil tools for empty input, got %v", tools)
	}
}

func TestOpenAIBuildTooling_SingleTool(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	schema := json.RawMessage(`{"type": "object", "properties": {"query": {"type": "string"}}}`)
	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "search",
			Description: "Search for information",
			InputSchema: schema,
		},
	}

	toolsInterface, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if toolsInterface == nil {
		t.Fatal("Expected non-nil tools")
	}

	tools, ok := toolsInterface.([]openAITool)
	if !ok {
		t.Fatalf("Expected []openAITool, got %T", toolsInterface)
	}

	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Type != "function" {
		t.Errorf("Expected type 'function', got '%s'", tool.Type)
	}

	if tool.Function.Name != "search" {
		t.Errorf("Expected name 'search', got '%s'", tool.Function.Name)
	}

	if tool.Function.Description != "Search for information" {
		t.Errorf("Expected description 'Search for information', got '%s'", tool.Function.Description)
	}

	if string(tool.Function.Parameters) != string(schema) {
		t.Errorf("Expected parameters %s, got %s", schema, tool.Function.Parameters)
	}
}

func TestOpenAIBuildTooling_MultipleTools(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	schema1 := json.RawMessage(`{"type": "object", "properties": {"query": {"type": "string"}}}`)
	schema2 := json.RawMessage(`{"type": "object", "properties": {"code": {"type": "string"}}}`)

	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "search",
			Description: "Search for information",
			InputSchema: schema1,
		},
		{
			Name:        "execute",
			Description: "Execute code",
			InputSchema: schema2,
		},
	}

	toolsInterface, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	tools, ok := toolsInterface.([]openAITool)
	if !ok {
		t.Fatalf("Expected []openAITool, got %T", toolsInterface)
	}

	if len(tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(tools))
	}

	// Check first tool
	if tools[0].Function.Name != "search" {
		t.Errorf("Expected first tool name 'search', got '%s'", tools[0].Function.Name)
	}

	// Check second tool
	if tools[1].Function.Name != "execute" {
		t.Errorf("Expected second tool name 'execute', got '%s'", tools[1].Function.Name)
	}
}

func TestOpenAIBuildTooling_ComplexSchema(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"},
			"tags": {
				"type": "array",
				"items": {"type": "string"}
			}
		},
		"required": ["name"]
	}`)

	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "createUser",
			Description: "Create a new user",
			InputSchema: schema,
		},
	}

	toolsInterface, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	tools, ok := toolsInterface.([]openAITool)
	if !ok {
		t.Fatalf("Expected []openAITool, got %T", toolsInterface)
	}

	// Verify complex schema is preserved
	var schemaObj map[string]interface{}
	if err := json.Unmarshal(tools[0].Function.Parameters, &schemaObj); err != nil {
		t.Fatalf("Failed to unmarshal schema: %v", err)
	}

	if schemaObj["type"] != "object" {
		t.Error("Schema type not preserved")
	}

	props, ok := schemaObj["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Properties not preserved in schema")
	}

	if len(props) != 3 {
		t.Errorf("Expected 3 properties, got %d", len(props))
	}
}

// ============================================================================
// parseToolResponse Tests
// ============================================================================

func TestOpenAIParseToolResponse_NoToolCalls(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello! How can I help you today?"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30
		}
	}`

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if chatResp.Content != "Hello! How can I help you today?" {
		t.Errorf("Expected content 'Hello! How can I help you today?', got '%s'", chatResp.Content)
	}

	if len(toolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(toolCalls))
	}

	if chatResp.CostInfo.InputTokens != 10 {
		t.Errorf("Expected 10 input tokens, got %d", chatResp.CostInfo.InputTokens)
	}

	if chatResp.CostInfo.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", chatResp.CostInfo.OutputTokens)
	}
}

func TestOpenAIParseToolResponse_WithToolCalls(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_123",
					"type": "function",
					"function": {
						"name": "search",
						"arguments": "{\"query\": \"weather\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {
			"prompt_tokens": 50,
			"completion_tokens": 30,
			"total_tokens": 80
		}
	}`

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls))
	}

	toolCall := toolCalls[0]
	if toolCall.ID != "call_123" {
		t.Errorf("Expected ID 'call_123', got '%s'", toolCall.ID)
	}

	if toolCall.Name != "search" {
		t.Errorf("Expected name 'search', got '%s'", toolCall.Name)
	}

	// Verify arguments
	var args map[string]interface{}
	if err := json.Unmarshal(toolCall.Args, &args); err != nil {
		t.Fatalf("Failed to unmarshal args: %v", err)
	}

	if args["query"] != "weather" {
		t.Errorf("Expected query 'weather', got %v", args["query"])
	}

	if chatResp.CostInfo.InputTokens != 50 {
		t.Errorf("Expected 50 input tokens, got %d", chatResp.CostInfo.InputTokens)
	}

	if chatResp.CostInfo.OutputTokens != 30 {
		t.Errorf("Expected 30 output tokens, got %d", chatResp.CostInfo.OutputTokens)
	}
}

func TestOpenAIParseToolResponse_MultipleToolCalls(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "search",
							"arguments": "{\"query\": \"weather\"}"
						}
					},
					{
						"id": "call_2",
						"type": "function",
						"function": {
							"name": "calculate",
							"arguments": "{\"expression\": \"2+2\"}"
						}
					}
				]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 50,
			"total_tokens": 150
		}
	}`

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(toolCalls) != 2 {
		t.Fatalf("Expected 2 tool calls, got %d", len(toolCalls))
	}

	// Check first tool call
	if toolCalls[0].Name != "search" {
		t.Errorf("Expected first tool 'search', got '%s'", toolCalls[0].Name)
	}

	// Check second tool call
	if toolCalls[1].Name != "calculate" {
		t.Errorf("Expected second tool 'calculate', got '%s'", toolCalls[1].Name)
	}

	if chatResp.CostInfo.InputTokens != 100 {
		t.Errorf("Expected 100 input tokens, got %d", chatResp.CostInfo.InputTokens)
	}
}

func TestOpenAIParseToolResponse_InvalidJSON(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{invalid json`

	_, _, err := provider.parseToolResponse([]byte(responseJSON))
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestOpenAIParseToolResponse_MissingFields(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "chatcmpl-123",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "test"
			}
		}]
	}`

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error for missing usage, got %v", err)
	}

	if chatResp.Content != "test" {
		t.Errorf("Expected content 'test', got '%s'", chatResp.Content)
	}

	if len(toolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(toolCalls))
	}

	// Token counts should be 0 if usage is missing
	if chatResp.CostInfo.InputTokens != 0 {
		t.Errorf("Expected 0 input tokens, got %d", chatResp.CostInfo.InputTokens)
	}
}

func TestOpenAIParseToolResponse_CachedTokens(t *testing.T) {
	provider := NewOpenAIToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello!"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 20,
			"total_tokens": 120,
			"prompt_tokens_details": {
				"cached_tokens": 50
			}
		}
	}`

	chatResp, _, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Total prompt tokens is 100, with 50 cached
	// So InputTokens (non-cached) should be 50
	if chatResp.CostInfo.InputTokens != 50 {
		t.Errorf("Expected 50 non-cached input tokens, got %d", chatResp.CostInfo.InputTokens)
	}

	if chatResp.CostInfo.CachedTokens != 50 {
		t.Errorf("Expected 50 cached tokens, got %d", chatResp.CostInfo.CachedTokens)
	}

	if chatResp.CostInfo.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", chatResp.CostInfo.OutputTokens)
	}

	// Verify the response is parsed correctly
	if chatResp.Content != "Hello!" {
		t.Errorf("Expected content 'Hello!', got '%s'", chatResp.Content)
	}
}

// ============================================================================
// OpenAI Tool Structure Tests
// ============================================================================

func TestOpenAIToolStructure(t *testing.T) {
	tool := openAITool{
		Type: "function",
		Function: openAIToolFunction{
			Name:        "test_function",
			Description: "A test function",
			Parameters:  json.RawMessage(`{"type": "object"}`),
		},
	}

	// Marshal and unmarshal to verify structure
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("Failed to marshal tool: %v", err)
	}

	var unmarshaled openAITool
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal tool: %v", err)
	}

	if unmarshaled.Type != "function" {
		t.Errorf("Expected type 'function', got '%s'", unmarshaled.Type)
	}

	if unmarshaled.Function.Name != "test_function" {
		t.Errorf("Expected name 'test_function', got '%s'", unmarshaled.Function.Name)
	}
}

func TestOpenAIToolCallStructure(t *testing.T) {
	toolCall := openAIToolCall{
		ID:   "call_123",
		Type: "function",
		Function: openAIFunctionCall{
			Name:      "search",
			Arguments: `{"query": "test"}`,
		},
	}

	// Marshal and unmarshal to verify structure
	data, err := json.Marshal(toolCall)
	if err != nil {
		t.Fatalf("Failed to marshal tool call: %v", err)
	}

	var unmarshaled openAIToolCall
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal tool call: %v", err)
	}

	if unmarshaled.ID != "call_123" {
		t.Errorf("Expected ID 'call_123', got '%s'", unmarshaled.ID)
	}

	if unmarshaled.Function.Name != "search" {
		t.Errorf("Expected name 'search', got '%s'", unmarshaled.Function.Name)
	}

	// Verify arguments is a string
	if unmarshaled.Function.Arguments != `{"query": "test"}` {
		t.Errorf("Expected arguments string, got '%s'", unmarshaled.Function.Arguments)
	}
}
