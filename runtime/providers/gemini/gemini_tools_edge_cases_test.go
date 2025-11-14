package gemini

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestGeminiToolProvider_BuildMessageParts_EmptyToolResults(t *testing.T) {
	msg := types.Message{
		Role:    "user",
		Content: "Hello",
	}

	parts := buildMessageParts(msg, []map[string]interface{}{})
	if len(parts) != 1 {
		t.Errorf("Expected 1 part, got %d", len(parts))
	}
}

func TestGeminiToolProvider_BuildMessageParts_WithToolResults(t *testing.T) {
	msg := types.Message{
		Role:    "user",
		Content: "Process result",
	}

	toolResults := []map[string]interface{}{
		{
			"functionResponse": map[string]interface{}{
				"name":     "get_weather",
				"response": map[string]interface{}{"temp": 72},
			},
		},
	}

	parts := buildMessageParts(msg, toolResults)
	// Should have tool result + text
	if len(parts) != 2 {
		t.Errorf("Expected 2 parts (tool result + text), got %d", len(parts))
	}
}

func TestGeminiToolProvider_BuildMessageParts_WithToolCalls(t *testing.T) {
	msg := types.Message{
		Role:    "assistant",
		Content: "Let me check that",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_1",
				Name: "search",
				Args: json.RawMessage(`{"query": "test"}`),
			},
		},
	}

	parts := buildMessageParts(msg, []map[string]interface{}{})
	// Should have text + function call
	if len(parts) < 2 {
		t.Errorf("Expected at least 2 parts (text + function call), got %d", len(parts))
	}
}

func TestGeminiToolProvider_ProcessToolMessage_EmptyName(t *testing.T) {
	msg := types.Message{
		Role:    "tool",
		Content: `{"result": "success"}`,
		ToolResult: &types.MessageToolResult{
			ID:      "call_1",
			Name:    "", // Empty name
			Content: `{"result": "success"}`,
		},
	}

	result := processToolMessage(msg)
	
	// Should still return a valid structure even with empty name
	if result == nil {
		t.Error("Expected non-nil result")
	}
	
	funcResp, ok := result["functionResponse"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected functionResponse map")
	}
	
	if funcResp["name"] != "" {
		t.Errorf("Expected empty name to be preserved")
	}
}

func TestGeminiToolProvider_ProcessToolMessage_StringContent(t *testing.T) {
	msg := types.Message{
		Role:    "tool",
		Content: "plain string result",
		ToolResult: &types.MessageToolResult{
			ID:      "call_1",
			Name:    "test_tool",
			Content: "plain string result",
		},
	}

	result := processToolMessage(msg)
	funcResp := result["functionResponse"].(map[string]interface{})
	response := funcResp["response"].(map[string]interface{})
	
	// Plain string should be wrapped in result field
	if response["result"] != "plain string result" {
		t.Error("Expected string content to be wrapped in result field")
	}
}

func TestGeminiToolProvider_ProcessToolMessage_PrimitiveContent(t *testing.T) {
	msg := types.Message{
		Role:    "tool",
		Content: "42", // Numeric string
		ToolResult: &types.MessageToolResult{
			ID:      "call_1",
			Name:    "get_number",
			Content: "42",
		},
	}

	result := processToolMessage(msg)
	funcResp := result["functionResponse"].(map[string]interface{})
	response := funcResp["response"].(map[string]interface{})
	
	// Primitive should be wrapped
	if response["result"] != float64(42) {
		t.Errorf("Expected number 42, got %v", response["result"])
	}
}

func TestGeminiToolProvider_AddToolConfig_AutoMode(t *testing.T) {
	request := make(map[string]interface{})
	tools := map[string]interface{}{"functions": []interface{}{}}
	
	addToolConfig(request, tools, "auto")
	
	toolConfig, ok := request["tool_config"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected tool_config to be set")
	}
	
	funcConfig, ok := toolConfig["function_calling_config"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected function_calling_config")
	}
	
	if funcConfig["mode"] != "AUTO" {
		t.Errorf("Expected AUTO mode, got %v", funcConfig["mode"])
	}
}

func TestGeminiToolProvider_AddToolConfig_RequiredMode(t *testing.T) {
	request := make(map[string]interface{})
	tools := map[string]interface{}{"functions": []interface{}{}}
	
	addToolConfig(request, tools, "required")
	
	toolConfig := request["tool_config"].(map[string]interface{})
	funcConfig := toolConfig["function_calling_config"].(map[string]interface{})
	
	if funcConfig["mode"] != "ANY" {
		t.Errorf("Expected ANY mode for required, got %v", funcConfig["mode"])
	}
}

func TestGeminiToolProvider_AddToolConfig_AnyMode(t *testing.T) {
	request := make(map[string]interface{})
	tools := map[string]interface{}{"functions": []interface{}{}}
	
	addToolConfig(request, tools, "any")
	
	toolConfig := request["tool_config"].(map[string]interface{})
	funcConfig := toolConfig["function_calling_config"].(map[string]interface{})
	
	if funcConfig["mode"] != "ANY" {
		t.Errorf("Expected ANY mode, got %v", funcConfig["mode"])
	}
}

func TestGeminiToolProvider_AddToolConfig_NoneMode(t *testing.T) {
	request := make(map[string]interface{})
	tools := map[string]interface{}{"functions": []interface{}{}}
	
	addToolConfig(request, tools, "none")
	
	toolConfig := request["tool_config"].(map[string]interface{})
	funcConfig := toolConfig["function_calling_config"].(map[string]interface{})
	
	if funcConfig["mode"] != "NONE" {
		t.Errorf("Expected NONE mode, got %v", funcConfig["mode"])
	}
}

func TestGeminiToolProvider_ParseToolResponse_MaxTokensError(t *testing.T) {
	provider := NewGeminiToolProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)
	
	// Response with MAX_TOKENS finish reason
	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "MAX_TOKENS"
		}]
	}`
	
	_, _, err := provider.parseToolResponse([]byte(respJSON), providers.PredictionResponse{})
	if err == nil {
		t.Error("Expected error for MAX_TOKENS finish reason")
	}
	
	if err != nil && err.Error() != "gemini returned MAX_TOKENS error (this should not happen with reasonable limits)" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestGeminiToolProvider_ParseToolResponse_SafetyError(t *testing.T) {
	provider := NewGeminiToolProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)
	
	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "SAFETY"
		}]
	}`
	
	_, _, err := provider.parseToolResponse([]byte(respJSON), providers.PredictionResponse{})
	if err == nil {
		t.Error("Expected error for SAFETY finish reason")
	}
	
	if err != nil && err.Error() != "response blocked by Gemini safety filters" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestGeminiToolProvider_ParseToolResponse_RecitationError(t *testing.T) {
	provider := NewGeminiToolProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)
	
	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "RECITATION"
		}]
	}`
	
	_, _, err := provider.parseToolResponse([]byte(respJSON), providers.PredictionResponse{})
	if err == nil {
		t.Error("Expected error for RECITATION finish reason")
	}
	
	if err != nil && err.Error() != "response blocked due to recitation concerns" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestGeminiToolProvider_ParseToolResponse_UnknownFinishReason(t *testing.T) {
	provider := NewGeminiToolProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)
	
	respJSON := `{
		"candidates": [{
			"content": {"parts": []},
			"finishReason": "UNKNOWN_REASON"
		}]
	}`
	
	_, _, err := provider.parseToolResponse([]byte(respJSON), providers.PredictionResponse{})
	if err == nil {
		t.Error("Expected error for unknown finish reason with no parts")
	}
}

func TestGeminiToolProvider_MakeRequest_NoCachedTokens(t *testing.T) {
	provider := NewGeminiToolProvider("test", "gemini-1.5-flash", "https://invalid.test", providers.ProviderDefaults{}, false)
	
	request := map[string]interface{}{
		"contents": []interface{}{},
	}
	
	// This will fail due to invalid URL, but we're testing that the code path doesn't crash
	_, err := provider.makeRequest(context.Background(), request)
	if err == nil {
		t.Error("Expected error for invalid request")
	}
}

func TestGeminiToolProvider_BuildToolRequest_EmptyMessages(t *testing.T) {
	provider := NewGeminiToolProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)
	
	req := providers.PredictionRequest{
		Messages:    []types.Message{},
		Temperature: 0.7,
		MaxTokens:   1000,
	}
	
	tools := []interface{}{map[string]interface{}{"name": "test"}}
	
	geminiReq := provider.buildToolRequest(req, tools, "auto")
	
	contents, ok := geminiReq["contents"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected contents to be present")
	}
	
	if len(contents) != 0 {
		t.Errorf("Expected empty contents, got %d", len(contents))
	}
}

func TestGeminiToolProvider_BuildToolRequest_NilTools(t *testing.T) {
	provider := NewGeminiToolProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)
	
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}
	
	geminiReq := provider.buildToolRequest(req, nil, "")
	
	// Should not have tools or tool_config
	if _, hasTools := geminiReq["tools"]; hasTools {
		t.Error("Should not have tools when nil")
	}
	
	if _, hasConfig := geminiReq["tool_config"]; hasConfig {
		t.Error("Should not have tool_config when tools are nil")
	}
}
