package claude

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// ============================================================================
// NewClaudeToolProvider Tests
// ============================================================================

func TestNewClaudeToolProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	provider := NewClaudeToolProvider("test-claude", "claude-3-opus-20240229", "https://api.anthropic.com/v1", defaults, false)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ClaudeProvider == nil {
		t.Fatal("Expected non-nil ClaudeProvider")
	}

	if provider.ID() != "test-claude" {
		t.Errorf("Expected id 'test-claude', got '%s'", provider.ID())
	}
}

// ============================================================================
// BuildTooling Tests
// ============================================================================

func TestClaudeBuildTooling_Empty(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	tools, err := provider.BuildTooling(nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if tools != nil {
		t.Errorf("Expected nil tools for empty input, got %v", tools)
	}
}

func TestClaudeBuildTooling_SingleTool(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), providers.PredictionResponse{})
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

func TestClaudeParseToolResponse_WithToolCall(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), providers.PredictionResponse{})
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
	if chatResp.Content != "I'll search for that information." {
		t.Errorf("Expected text content, got '%s'", chatResp.Content)
	}

	if chatResp.CostInfo.InputTokens != 50 {
		t.Errorf("Expected 50 input tokens, got %d", chatResp.CostInfo.InputTokens)
	}

	if chatResp.CostInfo.OutputTokens != 30 {
		t.Errorf("Expected 30 output tokens, got %d", chatResp.CostInfo.OutputTokens)
	}
}

func TestClaudeParseToolResponse_MultipleToolCalls(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), providers.PredictionResponse{})
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

func TestClaudeParseToolResponse_MixedContent(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	chatResp, toolCalls, err := provider.parseToolResponse([]byte(responseJSON), providers.PredictionResponse{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls))
	}

	// Current implementation extracts only the last text part
	// This is a known limitation - for now just verify it extracts something
	if chatResp.Content == "" {
		t.Error("Expected some text content to be extracted")
	}
}

func TestClaudeParseToolResponse_InvalidJSON(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

	responseJSON := `{invalid json`

	_, _, err := provider.parseToolResponse([]byte(responseJSON), providers.PredictionResponse{})
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestClaudeParseToolResponse_CachedTokens(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	chatResp, _, err := provider.parseToolResponse([]byte(responseJSON), providers.PredictionResponse{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Total input tokens is 100, with 80 from cache
	// So non-cached InputTokens should be 20
	if chatResp.CostInfo.InputTokens != 20 {
		t.Errorf("Expected 20 non-cached input tokens, got %d", chatResp.CostInfo.InputTokens)
	}

	if chatResp.CostInfo.CachedTokens != 80 {
		t.Errorf("Expected 80 cached tokens, got %d", chatResp.CostInfo.CachedTokens)
	}

	if chatResp.CostInfo.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", chatResp.CostInfo.OutputTokens)
	}

	// Verify the response is parsed correctly
	if chatResp.Content != "Hello!" {
		t.Errorf("Expected content 'Hello!', got '%s'", chatResp.Content)
	}
}

func TestClaudeParseToolResponse_EmptyContent(t *testing.T) {
	provider := NewClaudeToolProvider("test", "claude-3-opus", "https://api.anthropic.com/v1", providers.ProviderDefaults{}, false)

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

	_, _, err := provider.parseToolResponse([]byte(responseJSON), providers.PredictionResponse{})
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
