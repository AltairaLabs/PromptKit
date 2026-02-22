package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ============================================================================
// NewToolProvider Tests
// ============================================================================

func TestNewToolProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	provider := NewToolProvider("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false, nil)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.Provider == nil {
		t.Fatal("Expected non-nil OpenAIProvider")
	}

	if provider.ID() != "test-openai" {
		t.Errorf("Expected id 'test-openai', got '%s'", provider.ID())
	}
}

func TestNewToolProviderWithCredential(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	t.Run("with credential", func(t *testing.T) {
		cred := &mockCredential{credType: "api_key"}
		provider := NewToolProviderWithCredential("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false, nil, cred, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.Provider == nil {
			t.Fatal("Expected non-nil OpenAIProvider")
		}

		if provider.ID() != "test-openai" {
			t.Errorf("Expected id 'test-openai', got '%s'", provider.ID())
		}
	})

	t.Run("with nil credential", func(t *testing.T) {
		provider := NewToolProviderWithCredential("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false, nil, nil, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}
	})

	t.Run("with additional config", func(t *testing.T) {
		additionalConfig := map[string]any{"key": "value"}
		provider := NewToolProviderWithCredential("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false, additionalConfig, nil, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}
	})
}

// ============================================================================
// BuildTooling Tests
// ============================================================================

func TestOpenAIBuildTooling_Empty(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	tools, err := provider.BuildTooling(nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if tools != nil {
		t.Errorf("Expected nil tools for empty input, got %v", tools)
	}
}

func TestOpenAIBuildTooling_SingleTool(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

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
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "predictcmpl-123",
		"object": "predict.completion",
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

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if predictResp.Content != "Hello! How can I help you today?" {
		t.Errorf("Expected content 'Hello! How can I help you today?', got '%s'", predictResp.Content)
	}

	if len(toolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(toolCalls))
	}

	if predictResp.CostInfo.InputTokens != 10 {
		t.Errorf("Expected 10 input tokens, got %d", predictResp.CostInfo.InputTokens)
	}

	if predictResp.CostInfo.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", predictResp.CostInfo.OutputTokens)
	}
}

func TestOpenAIParseToolResponse_WithToolCalls(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "predictcmpl-123",
		"object": "predict.completion",
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

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
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

	if predictResp.CostInfo.InputTokens != 50 {
		t.Errorf("Expected 50 input tokens, got %d", predictResp.CostInfo.InputTokens)
	}

	if predictResp.CostInfo.OutputTokens != 30 {
		t.Errorf("Expected 30 output tokens, got %d", predictResp.CostInfo.OutputTokens)
	}
}

func TestOpenAIParseToolResponse_MultipleToolCalls(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "predictcmpl-123",
		"object": "predict.completion",
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

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
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

	if predictResp.CostInfo.InputTokens != 100 {
		t.Errorf("Expected 100 input tokens, got %d", predictResp.CostInfo.InputTokens)
	}
}

func TestOpenAIParseToolResponse_InvalidJSON(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{invalid json`

	_, _, err := provider.parseToolResponse([]byte(responseJSON))
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestOpenAIParseToolResponse_MissingFields(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "predictcmpl-123",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "test"
			}
		}]
	}`

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error for missing usage, got %v", err)
	}

	if predictResp.Content != "test" {
		t.Errorf("Expected content 'test', got '%s'", predictResp.Content)
	}

	if len(toolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(toolCalls))
	}

	// Token counts should be 0 if usage is missing
	if predictResp.CostInfo.InputTokens != 0 {
		t.Errorf("Expected 0 input tokens, got %d", predictResp.CostInfo.InputTokens)
	}
}

func TestOpenAIParseToolResponse_CachedTokens(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	responseJSON := `{
		"id": "predictcmpl-123",
		"object": "predict.completion",
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

	predictResp, _, err := provider.parseToolResponse([]byte(responseJSON))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Total prompt tokens is 100, with 50 cached
	// So InputTokens (non-cached) should be 50
	if predictResp.CostInfo.InputTokens != 50 {
		t.Errorf("Expected 50 non-cached input tokens, got %d", predictResp.CostInfo.InputTokens)
	}

	if predictResp.CostInfo.CachedTokens != 50 {
		t.Errorf("Expected 50 cached tokens, got %d", predictResp.CostInfo.CachedTokens)
	}

	if predictResp.CostInfo.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", predictResp.CostInfo.OutputTokens)
	}

	// Verify the response is parsed correctly
	if predictResp.Content != "Hello!" {
		t.Errorf("Expected content 'Hello!', got '%s'", predictResp.Content)
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

// ============================================================================
// addToolChoiceToRequest Tests
// ============================================================================

func TestToolProvider_AddToolChoiceToRequest(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	tests := []struct {
		name       string
		toolChoice string
		want       interface{}
	}{
		{
			name:       "empty string - no tool_choice added",
			toolChoice: "",
			want:       nil,
		},
		{
			name:       "auto - no tool_choice added",
			toolChoice: "auto",
			want:       nil,
		},
		{
			name:       "required",
			toolChoice: "required",
			want:       "required",
		},
		{
			name:       "none",
			toolChoice: "none",
			want:       "none",
		},
		{
			name:       "specific function name",
			toolChoice: "search_function",
			want: map[string]interface{}{
				"type": "function",
				"function": map[string]string{
					"name": "search_function",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openaiReq := make(map[string]interface{})
			provider.addToolChoiceToRequest(openaiReq, tt.toolChoice)

			if tt.want == nil {
				if _, exists := openaiReq["tool_choice"]; exists {
					t.Errorf("Expected no tool_choice, but found %v", openaiReq["tool_choice"])
				}
			} else {
				got, exists := openaiReq["tool_choice"]
				if !exists {
					t.Fatal("Expected tool_choice to be set")
				}

				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(tt.want)
				if string(gotJSON) != string(wantJSON) {
					t.Errorf("Expected tool_choice %s, got %s", wantJSON, gotJSON)
				}
			}
		})
	}
}

// ============================================================================
// BuildToolRequest Defaults Tests
// ============================================================================

func TestOpenAIToolProvider_BuildToolRequest_AppliesDefaults(t *testing.T) {
	tests := []struct {
		name              string
		reqTemp           float32
		reqTopP           float32
		reqMaxTokens      int
		defaultTemp       float32
		defaultTopP       float32
		defaultMaxTokens  int
		expectedTemp      float32
		expectedTopP      float32
		expectedMaxTokens int
	}{
		{
			name:              "Uses request values when provided",
			reqTemp:           0.8,
			reqTopP:           0.95,
			reqMaxTokens:      500,
			defaultTemp:       0.7,
			defaultTopP:       0.9,
			defaultMaxTokens:  1000,
			expectedTemp:      0.8,
			expectedTopP:      0.95,
			expectedMaxTokens: 500,
		},
		{
			name:              "Falls back to defaults for zero values",
			reqTemp:           0,
			reqTopP:           0,
			reqMaxTokens:      0,
			defaultTemp:       0.7,
			defaultTopP:       0.9,
			defaultMaxTokens:  2000,
			expectedTemp:      0.7,
			expectedTopP:      0.9,
			expectedMaxTokens: 2000,
		},
		{
			name:              "Mixed values - some request, some defaults",
			reqTemp:           0.6,
			reqTopP:           0,
			reqMaxTokens:      1500,
			defaultTemp:       0.5,
			defaultTopP:       0.92,
			defaultMaxTokens:  1000,
			expectedTemp:      0.6,
			expectedTopP:      0.92,
			expectedMaxTokens: 1500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewToolProvider(
				"test",
				"gpt-4",
				"https://api.openai.com/v1",
				providers.ProviderDefaults{
					Temperature: tt.defaultTemp,
					TopP:        tt.defaultTopP,
					MaxTokens:   tt.defaultMaxTokens,
				},
				false,
				nil,
			)

			req := providers.PredictionRequest{
				Temperature: tt.reqTemp,
				TopP:        tt.reqTopP,
				MaxTokens:   tt.reqMaxTokens,
				Messages:    []types.Message{{Role: "user", Content: "Hello"}},
			}

			request := provider.buildToolRequest(req, nil, "")

			if temp, ok := request["temperature"].(float32); !ok || temp != tt.expectedTemp {
				t.Errorf("Expected temperature %.2f, got %v", tt.expectedTemp, request["temperature"])
			}

			if topP, ok := request["top_p"].(float32); !ok || topP != tt.expectedTopP {
				t.Errorf("Expected top_p %.2f, got %v", tt.expectedTopP, request["top_p"])
			}

			if maxTokens, ok := request["max_tokens"].(int); !ok || maxTokens != tt.expectedMaxTokens {
				t.Errorf("Expected max_tokens %d, got %v", tt.expectedMaxTokens, request["max_tokens"])
			}
		})
	}
}

// ============================================================================
// PredictStreamWithTools Tests
// ============================================================================

func TestToolProvider_PredictStreamWithTools_BuildsRequestWithTools(t *testing.T) {
	// This test verifies that PredictStreamWithTools properly includes tools in the request
	// We can't test the actual streaming without a mock server, but we can verify the method exists
	// and has the correct signature

	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Verify the provider implements the interface with PredictStreamWithTools
	var _ providers.ToolSupport = provider

	// Build tools
	schema := json.RawMessage(`{"type": "object", "properties": {"query": {"type": "string"}}}`)
	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "search",
			Description: "Search for information",
			InputSchema: schema,
		},
	}

	tools, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Failed to build tooling: %v", err)
	}

	if tools == nil {
		t.Fatal("Expected non-nil tools")
	}

	// Verify tools are properly formatted for OpenAI
	openAITools, ok := tools.([]openAITool)
	if !ok {
		t.Fatalf("Expected []openAITool, got %T", tools)
	}

	if len(openAITools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(openAITools))
	}

	if openAITools[0].Function.Name != "search" {
		t.Errorf("Expected tool name 'search', got '%s'", openAITools[0].Function.Name)
	}
}

func TestToolProvider_PredictStreamWithTools_ImplementsToolSupport(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Verify it implements ToolSupport interface which now includes PredictStreamWithTools
	var toolSupport providers.ToolSupport = provider

	// Verify the methods exist (compile-time check via interface)
	_ = toolSupport.BuildTooling
	_ = toolSupport.PredictWithTools
	// PredictStreamWithTools is part of the interface, so if this compiles, it's implemented
}

func TestToolProvider_PredictStreamWithTools_TextResponse(t *testing.T) {
	// Create mock SSE stream with tool-aware response
	sseData := `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request contains tools
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)

		if _, hasTools := req["tools"]; !hasTools {
			t.Error("Expected tools in request")
		}
		if _, hasStream := req["stream"]; !hasStream {
			t.Error("Expected stream in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	// Use Chat Completions API mode since mock returns that format
	additionalConfig := map[string]any{"api_mode": "completions"}
	provider := NewToolProvider("test", "gpt-4", server.URL, providers.ProviderDefaults{}, false, additionalConfig)

	schema := json.RawMessage(`{"type": "object", "properties": {"q": {"type": "string"}}}`)
	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{Name: "search", Description: "Search", InputSchema: schema},
	})

	ctx := context.Background()
	stream, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}, tools, "auto")

	if err != nil {
		t.Fatalf("PredictStreamWithTools failed: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks")
	}

	final := chunks[len(chunks)-1]
	if final.Content != "Hello world" {
		t.Errorf("Final content: got %q, want %q", final.Content, "Hello world")
	}
}

func TestToolProvider_PredictStreamWithTools_ToolCallResponse(t *testing.T) {
	// Create mock SSE stream with tool call
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"search","arguments":""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"test\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":15,"completion_tokens":10}}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	// Use Chat Completions API mode since mock returns that format
	additionalConfig := map[string]any{"api_mode": "completions"}
	provider := NewToolProvider("test", "gpt-4", server.URL, providers.ProviderDefaults{}, false, additionalConfig)

	schema := json.RawMessage(`{"type": "object", "properties": {"q": {"type": "string"}}}`)
	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{Name: "search", Description: "Search", InputSchema: schema},
	})

	ctx := context.Background()
	stream, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "search for test"}},
	}, tools, "auto")

	if err != nil {
		t.Fatalf("PredictStreamWithTools failed: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks")
	}

	final := chunks[len(chunks)-1]
	if final.FinishReason == nil || *final.FinishReason != "tool_calls" {
		t.Errorf("Expected finish_reason=tool_calls, got %v", final.FinishReason)
	}

	if len(final.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(final.ToolCalls))
	}

	if final.ToolCalls[0].Name != "search" {
		t.Errorf("Expected tool name 'search', got '%s'", final.ToolCalls[0].Name)
	}
}

func TestToolProvider_PredictStreamWithTools_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	provider := NewToolProvider("test", "gpt-4", server.URL, providers.ProviderDefaults{}, false, nil)

	ctx := context.Background()
	_, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}, nil, "auto")

	if err == nil {
		t.Fatal("Expected error for HTTP 500")
	}
}

// ============================================================================
// Tool Result Message Conversion Tests
// ============================================================================

// TestToolProvider_ConvertToolResultMessage_ContentIsSet verifies that tool result
// messages have their content properly set from ToolResult.Content when building
// the OpenAI request. This is critical for streaming with tools to work correctly.
func TestToolProvider_ConvertToolResultMessage_ContentIsSet(t *testing.T) {
	// This test verifies the bug fix: tool result messages must use ToolResult.Content
	// as the message content, not the (empty) Message.Content field.

	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	// Create a tool result message (as created by stages_provider.go executeToolCalls)
	// Note: Message.Content is NOT set - only ToolResult.Content has the data
	toolResultMsg := types.Message{
		Role: "tool",
		// Content is intentionally empty - this is how the SDK creates tool result messages
		ToolResult: &types.MessageToolResult{
			ID:      "call_123",
			Name:    "weather",
			Content: `{"temperature": 73, "conditions": "sunny"}`,
			Error:   "",
		},
	}

	// Convert to OpenAI format
	openaiMsg := provider.convertSingleMessageForTools(toolResultMsg)

	// Verify tool_call_id and name are set
	if openaiMsg["tool_call_id"] != "call_123" {
		t.Errorf("Expected tool_call_id 'call_123', got '%v'", openaiMsg["tool_call_id"])
	}
	if openaiMsg["name"] != "weather" {
		t.Errorf("Expected name 'weather', got '%v'", openaiMsg["name"])
	}

	// Verify content is set from ToolResult.Content (not empty)
	content, ok := openaiMsg["content"].(string)
	if !ok {
		t.Fatalf("Expected content to be string, got %T", openaiMsg["content"])
	}
	if content == "" {
		t.Fatal("Tool result content is empty - ToolResult.Content should be used as the message content")
	}
	if content != `{"temperature": 73, "conditions": "sunny"}` {
		t.Errorf("Expected content to be the tool result JSON, got '%s'", content)
	}
}

// TestToolProvider_ConvertRequestMessages_ToolResultHasContent verifies that when building
// messages for a request with tool results, the content is properly included.
func TestToolProvider_ConvertRequestMessages_ToolResultHasContent(t *testing.T) {
	provider := NewToolProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather in Miami?"},
			{
				Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{ID: "call_abc", Name: "weather", Args: json.RawMessage(`{"location": "Miami"}`)},
				},
			},
			{
				Role: "tool",
				// Content is NOT set - only ToolResult has the data
				ToolResult: &types.MessageToolResult{
					ID:      "call_abc",
					Name:    "weather",
					Content: `{"temp": 75, "conditions": "partly cloudy"}`,
				},
			},
		},
	}

	messages := provider.convertRequestMessagesToOpenAI(req)

	// Find the tool result message
	var toolMsg map[string]interface{}
	for _, msg := range messages {
		if msg["role"] == "tool" {
			toolMsg = msg
			break
		}
	}

	if toolMsg == nil {
		t.Fatal("Expected to find tool message in built messages")
	}

	// Verify content is set
	content, ok := toolMsg["content"].(string)
	if !ok {
		t.Fatalf("Expected content to be string, got %T", toolMsg["content"])
	}
	if content == "" {
		t.Fatal("Tool message content is empty in built request")
	}
	if content != `{"temp": 75, "conditions": "partly cloudy"}` {
		t.Errorf("Expected tool result content, got '%s'", content)
	}
}
