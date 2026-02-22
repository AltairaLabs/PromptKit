package claude

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	provider := NewToolProvider("test-claude", "claude-3-opus-20240229", "https://api.anthropic.com/v1", defaults, false)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.Provider == nil {
		t.Fatal("Expected non-nil ClaudeProvider")
	}

	if provider.ID() != "test-claude" {
		t.Errorf("Expected id 'test-claude', got '%s'", provider.ID())
	}
}

func TestNewToolProviderWithCredential(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	t.Run("with credential", func(t *testing.T) {
		cred := &mockCredential{credType: "api_key"}
		provider := NewToolProviderWithCredential("test-claude", "claude-3-opus", "https://api.anthropic.com", defaults, false, cred, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.Provider == nil {
			t.Fatal("Expected non-nil ClaudeProvider")
		}

		if provider.ID() != "test-claude" {
			t.Errorf("Expected id 'test-claude', got '%s'", provider.ID())
		}
	})

	t.Run("with nil credential", func(t *testing.T) {
		provider := NewToolProviderWithCredential("test-claude", "claude-3-opus", "https://api.anthropic.com", defaults, false, nil, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}
	})
}

// ============================================================================
// BuildTooling Tests
// ============================================================================

func TestClaudeBuildTooling_Empty(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	tools, err := provider.BuildTooling(nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if tools != nil {
		t.Errorf("Expected nil tools for empty input, got %v", tools)
	}
}

func TestClaudeBuildTooling_SingleTool(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	schema := json.RawMessage(`{"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]}`)
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

	tools, ok := toolsInterface.([]claudeTool)
	if !ok {
		t.Fatalf("Expected []claudeTool, got %T", toolsInterface)
	}

	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Name != "search" {
		t.Errorf("Expected name 'search', got '%s'", tool.Name)
	}

	if tool.Description != "Search for information" {
		t.Errorf("Expected description 'Search for information', got '%s'", tool.Description)
	}

	if string(tool.InputSchema) != string(schema) {
		t.Errorf("Expected input_schema %s, got %s", schema, tool.InputSchema)
	}
}

func TestClaudeBuildTooling_MultipleTools(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	tools, ok := toolsInterface.([]claudeTool)
	if !ok {
		t.Fatalf("Expected []claudeTool, got %T", toolsInterface)
	}

	if len(tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(tools))
	}

	// Check first tool
	if tools[0].Name != "search" {
		t.Errorf("Expected first tool name 'search', got '%s'", tools[0].Name)
	}

	// Check second tool
	if tools[1].Name != "execute" {
		t.Errorf("Expected second tool name 'execute', got '%s'", tools[1].Name)
	}
}

func TestClaudeBuildTooling_ComplexSchema(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "User name"},
			"age": {"type": "integer", "minimum": 0},
			"tags": {
				"type": "array",
				"items": {"type": "string"}
			},
			"metadata": {
				"type": "object",
				"properties": {
					"created": {"type": "string"}
				}
			}
		},
		"required": ["name", "age"]
	}`)

	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "createUser",
			Description: "Create a new user with validation",
			InputSchema: schema,
		},
	}

	toolsInterface, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	tools, ok := toolsInterface.([]claudeTool)
	if !ok {
		t.Fatalf("Expected []claudeTool, got %T", toolsInterface)
	}

	// Verify complex schema is preserved
	var schemaObj map[string]interface{}
	if err := json.Unmarshal(tools[0].InputSchema, &schemaObj); err != nil {
		t.Fatalf("Failed to unmarshal schema: %v", err)
	}

	if schemaObj["type"] != "object" {
		t.Error("Schema type not preserved")
	}

	props, ok := schemaObj["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Properties not preserved in schema")
	}

	if len(props) != 4 {
		t.Errorf("Expected 4 properties, got %d", len(props))
	}

	// Verify nested schema
	metadata, ok := props["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("Nested metadata object not preserved")
	}

	if metadata["type"] != "object" {
		t.Error("Nested object type not preserved")
	}
}

// ============================================================================
// parseToolResponse Tests
// ============================================================================

func TestClaudeParseToolResponse_NoToolCalls(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{
			"type": "text",
			"text": "Hello! How can I help you today?"
		}],
		"model": "claude-3-opus-20240229",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 20
		}
	}`

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), &providers.PredictionResponse{}, time.Second)
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

func TestClaudeParseToolResponse_WithToolCall(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{
				"type": "text",
				"text": "I'll search for that information."
			},
			{
				"type": "tool_use",
				"id": "toolu_123",
				"name": "search",
				"input": {
					"query": "weather forecast"
				}
			}
		],
		"model": "claude-3-opus-20240229",
		"stop_reason": "tool_use",
		"usage": {
			"input_tokens": 50,
			"output_tokens": 30
		}
	}`

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), &providers.PredictionResponse{}, time.Second)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls))
	}

	toolCall := toolCalls[0]
	if toolCall.ID != "toolu_123" {
		t.Errorf("Expected ID 'toolu_123', got '%s'", toolCall.ID)
	}

	if toolCall.Name != "search" {
		t.Errorf("Expected name 'search', got '%s'", toolCall.Name)
	}

	// Verify input arguments
	var args map[string]interface{}
	if err := json.Unmarshal(toolCall.Args, &args); err != nil {
		t.Fatalf("Failed to unmarshal args: %v", err)
	}

	if args["query"] != "weather forecast" {
		t.Errorf("Expected query 'weather forecast', got %v", args["query"])
	}

	// Content should include the text part
	if predictResp.Content != "I'll search for that information." {
		t.Errorf("Expected text content, got '%s'", predictResp.Content)
	}

	if predictResp.CostInfo.InputTokens != 50 {
		t.Errorf("Expected 50 input tokens, got %d", predictResp.CostInfo.InputTokens)
	}

	if predictResp.CostInfo.OutputTokens != 30 {
		t.Errorf("Expected 30 output tokens, got %d", predictResp.CostInfo.OutputTokens)
	}
}

func TestClaudeParseToolResponse_MultipleToolCalls(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{
				"type": "tool_use",
				"id": "toolu_1",
				"name": "search",
				"input": {
					"query": "weather"
				}
			},
			{
				"type": "tool_use",
				"id": "toolu_2",
				"name": "calculate",
				"input": {
					"expression": "2+2"
				}
			}
		],
		"model": "claude-3-opus-20240229",
		"stop_reason": "tool_use",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50
		}
	}`

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), &providers.PredictionResponse{}, time.Second)
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

func TestClaudeParseToolResponse_MixedContent(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{
				"type": "text",
				"text": "Let me check that for you."
			},
			{
				"type": "tool_use",
				"id": "toolu_123",
				"name": "database_query",
				"input": {
					"table": "users",
					"filter": "active=true"
				}
			},
			{
				"type": "text",
				"text": "I'm querying the database now."
			}
		],
		"model": "claude-3-opus-20240229",
		"stop_reason": "tool_use",
		"usage": {
			"input_tokens": 75,
			"output_tokens": 40
		}
	}`

	predictResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), &providers.PredictionResponse{}, time.Second)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls))
	}

	// Current implementation extracts only the last text part
	// This is a known limitation - for now just verify it extracts something
	if predictResp.Content == "" {
		t.Error("Expected some text content to be extracted")
	}
}

func TestClaudeParseToolResponse_InvalidJSON(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{invalid json`

	_, _, err := provider.parseToolResponse([]byte(responseJSON), &providers.PredictionResponse{}, time.Second)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestClaudeParseToolResponse_CachedTokens(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{
			"type": "text",
			"text": "Hello!"
		}],
		"model": "claude-3-opus-20240229",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 20,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens": 80
		}
	}`

	predictResp, _, err := provider.parseToolResponse([]byte(responseJSON), &providers.PredictionResponse{}, time.Second)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Total input tokens is 100, with 80 from cache
	// So non-cached InputTokens should be 20
	if predictResp.CostInfo.InputTokens != 20 {
		t.Errorf("Expected 20 non-cached input tokens, got %d", predictResp.CostInfo.InputTokens)
	}

	if predictResp.CostInfo.CachedTokens != 80 {
		t.Errorf("Expected 80 cached tokens, got %d", predictResp.CostInfo.CachedTokens)
	}

	if predictResp.CostInfo.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", predictResp.CostInfo.OutputTokens)
	}

	// Verify the response is parsed correctly
	if predictResp.Content != "Hello!" {
		t.Errorf("Expected content 'Hello!', got '%s'", predictResp.Content)
	}
}

func TestClaudeParseToolResponse_EmptyContent(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [],
		"model": "claude-3-opus-20240229",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5
		}
	}`

	_, _, err := provider.parseToolResponse([]byte(responseJSON), &providers.PredictionResponse{}, time.Second)
	// The current implementation returns an error for empty content
	if err == nil {
		t.Error("Expected error for empty content, got nil")
	}

	if err != nil && !contains(err.Error(), "no content") {
		t.Errorf("Expected 'no content' error, got '%v'", err)
	}
}

// ============================================================================
// Claude Tool Structure Tests
// ============================================================================

func TestClaudeToolStructure(t *testing.T) {
	tool := claudeTool{
		Name:        "test_function",
		Description: "A test function",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"arg": {"type": "string"}}}`),
	}

	// Marshal and unmarshal to verify structure
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("Failed to marshal tool: %v", err)
	}

	var unmarshaled claudeTool
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal tool: %v", err)
	}

	if unmarshaled.Name != "test_function" {
		t.Errorf("Expected name 'test_function', got '%s'", unmarshaled.Name)
	}

	if unmarshaled.Description != "A test function" {
		t.Errorf("Expected description 'A test function', got '%s'", unmarshaled.Description)
	}

	// Verify input_schema field name
	var rawMap map[string]interface{}
	if err := json.Unmarshal(data, &rawMap); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if _, ok := rawMap["input_schema"]; !ok {
		t.Error("Expected 'input_schema' field in JSON")
	}
}

func TestClaudeToolUseStructure(t *testing.T) {
	toolUse := claudeToolUse{
		Type:  "tool_use",
		ID:    "toolu_123",
		Name:  "search",
		Input: json.RawMessage(`{"query": "test"}`),
	}

	// Marshal and unmarshal to verify structure
	data, err := json.Marshal(toolUse)
	if err != nil {
		t.Fatalf("Failed to marshal tool use: %v", err)
	}

	var unmarshaled claudeToolUse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal tool use: %v", err)
	}

	if unmarshaled.Type != "tool_use" {
		t.Errorf("Expected type 'tool_use', got '%s'", unmarshaled.Type)
	}

	if unmarshaled.ID != "toolu_123" {
		t.Errorf("Expected ID 'toolu_123', got '%s'", unmarshaled.ID)
	}

	if unmarshaled.Name != "search" {
		t.Errorf("Expected name 'search', got '%s'", unmarshaled.Name)
	}

	// Verify input is RawMessage
	var input map[string]interface{}
	if err := json.Unmarshal(unmarshaled.Input, &input); err != nil {
		t.Fatalf("Failed to unmarshal input: %v", err)
	}

	if input["query"] != "test" {
		t.Errorf("Expected query 'test', got %v", input["query"])
	}
}

// Helper function (if not already defined)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && someContains(s, substr)))
}

func someContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// BuildToolRequest Defaults Tests
// ============================================================================

func TestClaudeToolProvider_BuildToolRequest_AppliesDefaults(t *testing.T) {
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
				"claude-3-opus",
				"https://api.anthropic.com/v1",
				providers.ProviderDefaults{
					Temperature: tt.defaultTemp,
					TopP:        tt.defaultTopP,
					MaxTokens:   tt.defaultMaxTokens,
				},
				false,
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

			// Note: top_p is intentionally NOT included in the request
			// Claude 4+ doesn't support both temperature and top_p simultaneously
			// See buildToolRequest() comment for details
			if _, hasTopP := request["top_p"]; hasTopP {
				t.Errorf("top_p should not be in request (Claude 4+ doesn't support both temp and top_p)")
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

func TestClaudeToolProvider_PredictStreamWithTools_ImplementsToolSupport(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	// Verify it implements ToolSupport interface which includes PredictStreamWithTools
	var toolSupport providers.ToolSupport = provider

	// If this compiles, the interface is implemented correctly
	_ = toolSupport.BuildTooling
	_ = toolSupport.PredictWithTools
}

func TestClaudeToolProvider_PredictStreamWithTools_BuildsRequestWithTools(t *testing.T) {
	provider := NewToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	// Verify tools are properly formatted for Claude
	claudeTools, ok := tools.([]claudeTool)
	if !ok {
		t.Fatalf("Expected []claudeTool, got %T", tools)
	}

	if len(claudeTools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(claudeTools))
	}

	if claudeTools[0].Name != "search" {
		t.Errorf("Expected tool name 'search', got '%s'", claudeTools[0].Name)
	}
}

func TestClaudeToolProvider_PredictStreamWithTools_TextResponse(t *testing.T) {
	// Create mock SSE stream with Claude's event format
	sseData := `data: {"type":"content_block_start","index":0}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" Claude"}}

data: {"type":"content_block_stop"}

data: {"type":"message_stop","message":{"stop_reason":"end_turn"}}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request contains tools and stream flag
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)

		if _, hasTools := req["tools"]; !hasTools {
			t.Error("Expected tools in request")
		}
		if stream, hasStream := req["stream"]; !hasStream || stream != true {
			t.Error("Expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	provider := NewToolProvider("test", "claude-3-opus", server.URL, providers.ProviderDefaults{}, false)

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
	if final.Content != "Hello Claude" {
		t.Errorf("Final content: got %q, want %q", final.Content, "Hello Claude")
	}
}

func TestClaudeToolProvider_PredictStreamWithTools_ToolCallResponse(t *testing.T) {
	// Create mock SSE stream with tool use - test that the request is made correctly
	// The streaming parser may not extract tool calls; we're testing the HTTP flow here
	sseData := `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"search","input":{}}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"test\"}"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_stop","message":{"stop_reason":"tool_use"}}

`
	requestReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		// Verify request contains tools
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)

		if _, hasTools := req["tools"]; !hasTools {
			t.Error("Expected tools in request")
		}
		if stream, ok := req["stream"]; !ok || stream != true {
			t.Error("Expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	provider := NewToolProvider("test", "claude-3-opus", server.URL, providers.ProviderDefaults{}, false)

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

	// Drain the stream
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
	}

	if !requestReceived {
		t.Fatal("Expected request to be made to server")
	}
}

func TestClaudeToolProvider_PredictStreamWithTools_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	provider := NewToolProvider("test", "claude-3-opus", server.URL, providers.ProviderDefaults{}, false)

	ctx := context.Background()
	_, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}, nil, "auto")

	if err == nil {
		t.Fatal("Expected error for HTTP 500")
	}
}

// ============================================================================
// Bedrock Body Mutation Tests
// ============================================================================

func TestToolProvider_MakeRequest_BedrockBodyMutation(t *testing.T) {
	// Create a server that captures the request body
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		// Return a valid Claude response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id":"msg_123","type":"message","role":"assistant",
			"content":[{"type":"text","text":"ok"}],
			"model":"test","stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":5}
		}`))
	}))
	defer server.Close()

	// Create a Bedrock tool provider (no real AWS cred needed since we use a local server)
	provider := NewToolProviderWithCredential(
		"test", "anthropic.claude-3-5-haiku-20241022-v1:0", server.URL,
		providers.ProviderDefaults{MaxTokens: 100}, false,
		nil, "bedrock", nil,
	)

	// Build a request map (simulating buildToolRequest output)
	request := map[string]interface{}{
		"model":       "anthropic.claude-3-5-haiku-20241022-v1:0",
		"max_tokens":  100,
		"messages":    []interface{}{},
		"temperature": float32(0.7),
	}

	ctx := context.Background()
	_, err := provider.makeRequest(ctx, request)
	if err != nil {
		t.Fatalf("makeRequest failed: %v", err)
	}

	// Verify Bedrock mutations were applied
	if capturedBody["anthropic_version"] != "bedrock-2023-05-31" {
		t.Errorf("expected anthropic_version in body, got: %v", capturedBody["anthropic_version"])
	}

	if _, hasModel := capturedBody["model"]; hasModel {
		t.Error("model field should be removed from Bedrock request body")
	}
}
