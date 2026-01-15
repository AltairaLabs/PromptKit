package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessage_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	msg := Message{
		Role:      "assistant",
		Content:   "Hello, world!",
		Timestamp: now,
		LatencyMs: 150,
		Meta: map[string]interface{}{
			"model": "gpt-4",
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	// Unmarshal back
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	// Verify fields
	if decoded.Role != msg.Role {
		t.Errorf("Role mismatch: got %q, want %q", decoded.Role, msg.Role)
	}
	if decoded.Content != msg.Content {
		t.Errorf("Content mismatch: got %q, want %q", decoded.Content, msg.Content)
	}
	if decoded.LatencyMs != msg.LatencyMs {
		t.Errorf("LatencyMs mismatch: got %d, want %d", decoded.LatencyMs, msg.LatencyMs)
	}
}

func TestMessage_SourceNotPersisted(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "test",
		Source:  "statestore",
	}

	// Marshal to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	// Source should not be in JSON due to json:"-" tag
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if _, exists := raw["source"]; exists {
		t.Error("Source field should not be persisted in JSON")
	}
}

func TestMessage_WithToolCalls(t *testing.T) {
	msg := Message{
		Role:    "assistant",
		Content: "Let me check that for you.",
		ToolCalls: []MessageToolCall{
			{
				ID:   "call_123",
				Name: "get_weather",
				Args: json.RawMessage(`{"location":"San Francisco"}`),
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with tool calls: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal message with tool calls: %v", err)
	}

	if len(decoded.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(decoded.ToolCalls))
	}

	tc := decoded.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("ToolCall ID mismatch: got %q, want %q", tc.ID, "call_123")
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall Name mismatch: got %q, want %q", tc.Name, "get_weather")
	}
}

func TestMessage_WithToolResult(t *testing.T) {
	msg := Message{
		Role:    "tool",
		Content: "",
		ToolResult: &MessageToolResult{
			ID:        "call_123",
			Name:      "get_weather",
			Content:   `{"temp":72,"condition":"sunny"}`,
			LatencyMs: 250,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with tool result: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal message with tool result: %v", err)
	}

	if decoded.ToolResult == nil {
		t.Fatal("Expected ToolResult to be present")
	}

	tr := decoded.ToolResult
	if tr.ID != "call_123" {
		t.Errorf("ToolResult ID mismatch: got %q, want %q", tr.ID, "call_123")
	}
	if tr.Name != "get_weather" {
		t.Errorf("ToolResult Name mismatch: got %q, want %q", tr.Name, "get_weather")
	}
	if tr.LatencyMs != 250 {
		t.Errorf("ToolResult LatencyMs mismatch: got %d, want %d", tr.LatencyMs, 250)
	}
}

func TestMessage_WithToolError(t *testing.T) {
	msg := Message{
		Role:    "tool",
		Content: "",
		ToolResult: &MessageToolResult{
			ID:      "call_456",
			Name:    "invalid_tool",
			Content: "",
			Error:   "Tool not found",
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with tool error: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal message with tool error: %v", err)
	}

	if decoded.ToolResult == nil {
		t.Fatal("Expected ToolResult to be present")
	}

	if decoded.ToolResult.Error != "Tool not found" {
		t.Errorf("ToolResult Error mismatch: got %q, want %q", decoded.ToolResult.Error, "Tool not found")
	}
}

func TestMessage_WithCostInfo(t *testing.T) {
	msg := Message{
		Role:    "assistant",
		Content: "Response text",
		CostInfo: &CostInfo{
			InputTokens:   100,
			OutputTokens:  50,
			InputCostUSD:  0.0001,
			OutputCostUSD: 0.0002,
			TotalCost:     0.0003,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with cost info: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal message with cost info: %v", err)
	}

	if decoded.CostInfo == nil {
		t.Fatal("Expected CostInfo to be present")
	}

	ci := decoded.CostInfo
	if ci.InputTokens != 100 {
		t.Errorf("InputTokens mismatch: got %d, want %d", ci.InputTokens, 100)
	}
	if ci.OutputTokens != 50 {
		t.Errorf("OutputTokens mismatch: got %d, want %d", ci.OutputTokens, 50)
	}
	if ci.TotalCost != 0.0003 {
		t.Errorf("TotalCost mismatch: got %f, want %f", ci.TotalCost, 0.0003)
	}
}

func TestMessage_WithValidations(t *testing.T) {
	now := time.Now().UTC()

	msg := Message{
		Role:    "assistant",
		Content: "Safe content",
		Validations: []ValidationResult{
			{
				ValidatorType: "*validators.BannedWordsValidator",
				Passed:        true,
				Details: map[string]interface{}{
					"checked_words": 2,
				},
				Timestamp: now,
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with validations: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal message with validations: %v", err)
	}

	if len(decoded.Validations) != 1 {
		t.Fatalf("Expected 1 validation result, got %d", len(decoded.Validations))
	}

	vr := decoded.Validations[0]
	if vr.ValidatorType != "*validators.BannedWordsValidator" {
		t.Errorf("ValidatorType mismatch: got %q", vr.ValidatorType)
	}
	if !vr.Passed {
		t.Error("Expected validation to have passed")
	}
}

func TestMessageToolCall_JSONMarshaling(t *testing.T) {
	tc := MessageToolCall{
		ID:   "call_789",
		Name: "search",
		Args: json.RawMessage(`{"query":"Go programming"}`),
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("Failed to marshal MessageToolCall: %v", err)
	}

	var decoded MessageToolCall
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal MessageToolCall: %v", err)
	}

	if decoded.ID != tc.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, tc.ID)
	}
	if decoded.Name != tc.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, tc.Name)
	}

	// Compare Args as strings
	if string(decoded.Args) != string(tc.Args) {
		t.Errorf("Args mismatch: got %q, want %q", string(decoded.Args), string(tc.Args))
	}
}

func TestToolDef_JSONMarshaling(t *testing.T) {
	td := ToolDef{
		Name:        "calculator",
		Description: "Performs mathematical calculations",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}}}`),
	}

	data, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("Failed to marshal ToolDef: %v", err)
	}

	var decoded ToolDef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ToolDef: %v", err)
	}

	if decoded.Name != td.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, td.Name)
	}
	if decoded.Description != td.Description {
		t.Errorf("Description mismatch: got %q, want %q", decoded.Description, td.Description)
	}
}

func TestCostInfo_Calculation(t *testing.T) {
	ci := CostInfo{
		InputTokens:   1000,
		OutputTokens:  500,
		CachedTokens:  200,
		InputCostUSD:  0.01,
		OutputCostUSD: 0.02,
		CachedCostUSD: 0.005,
		TotalCost:     0.035,
	}

	data, err := json.Marshal(ci)
	if err != nil {
		t.Fatalf("Failed to marshal CostInfo: %v", err)
	}

	var decoded CostInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal CostInfo: %v", err)
	}

	if decoded.InputTokens != ci.InputTokens {
		t.Errorf("InputTokens mismatch: got %d, want %d", decoded.InputTokens, ci.InputTokens)
	}
	if decoded.TotalCost != ci.TotalCost {
		t.Errorf("TotalCost mismatch: got %f, want %f", decoded.TotalCost, ci.TotalCost)
	}
}

func TestToolStats_JSONMarshaling(t *testing.T) {
	ts := ToolStats{
		TotalCalls: 5,
		ByTool: map[string]int{
			"get_weather": 3,
			"search":      2,
		},
	}

	data, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Failed to marshal ToolStats: %v", err)
	}

	var decoded ToolStats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ToolStats: %v", err)
	}

	if decoded.TotalCalls != ts.TotalCalls {
		t.Errorf("TotalCalls mismatch: got %d, want %d", decoded.TotalCalls, ts.TotalCalls)
	}
	if len(decoded.ByTool) != len(ts.ByTool) {
		t.Errorf("ByTool length mismatch: got %d, want %d", len(decoded.ByTool), len(ts.ByTool))
	}
	if decoded.ByTool["get_weather"] != 3 {
		t.Errorf("ByTool['get_weather'] mismatch: got %d, want %d", decoded.ByTool["get_weather"], 3)
	}
}

func TestValidationError_JSONMarshaling(t *testing.T) {
	ve := ValidationError{
		Type:   "args_invalid",
		Tool:   "calculator",
		Detail: "Expression cannot be empty",
	}

	data, err := json.Marshal(ve)
	if err != nil {
		t.Fatalf("Failed to marshal ValidationError: %v", err)
	}

	var decoded ValidationError
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ValidationError: %v", err)
	}

	if decoded.Type != ve.Type {
		t.Errorf("Type mismatch: got %q, want %q", decoded.Type, ve.Type)
	}
	if decoded.Tool != ve.Tool {
		t.Errorf("Tool mismatch: got %q, want %q", decoded.Tool, ve.Tool)
	}
	if decoded.Detail != ve.Detail {
		t.Errorf("Detail mismatch: got %q, want %q", decoded.Detail, ve.Detail)
	}
}

func TestValidationResult_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC()

	vr := ValidationResult{
		ValidatorType: "*validators.LengthValidator",
		Passed:        false,
		Details: map[string]interface{}{
			"max_length": 100,
			"actual":     150,
		},
		Timestamp: now,
	}

	data, err := json.Marshal(vr)
	if err != nil {
		t.Fatalf("Failed to marshal ValidationResult: %v", err)
	}

	var decoded ValidationResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ValidationResult: %v", err)
	}

	if decoded.ValidatorType != vr.ValidatorType {
		t.Errorf("ValidatorType mismatch: got %q, want %q", decoded.ValidatorType, vr.ValidatorType)
	}
	if decoded.Passed != vr.Passed {
		t.Errorf("Passed mismatch: got %v, want %v", decoded.Passed, vr.Passed)
	}
}

func TestMessage_EmptyOptionalFields(t *testing.T) {
	// Test that omitempty works correctly
	msg := Message{
		Role:    "user",
		Content: "Hello",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal minimal message: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	// These fields should not be present due to omitempty
	// Note: timestamp with zero value is omitted by Go's json encoder
	omitFields := []string{"tool_calls", "tool_result", "latency_ms", "cost_info", "meta", "validations"}
	for _, field := range omitFields {
		if _, exists := raw[field]; exists {
			t.Errorf("Field %q should be omitted when empty", field)
		}
	}

	// Timestamp may or may not be present depending on zero value handling
	// The zero time.Time is omitted in Go 1.13+
}

func TestCostInfo_WithCachedTokens(t *testing.T) {
	ci := CostInfo{
		InputTokens:   1000,
		OutputTokens:  500,
		CachedTokens:  300,
		InputCostUSD:  0.01,
		OutputCostUSD: 0.02,
		CachedCostUSD: 0.003,
		TotalCost:     0.033,
	}

	data, err := json.Marshal(ci)
	if err != nil {
		t.Fatalf("Failed to marshal CostInfo: %v", err)
	}

	var decoded CostInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal CostInfo: %v", err)
	}

	if decoded.CachedTokens != 300 {
		t.Errorf("CachedTokens mismatch: got %d, want %d", decoded.CachedTokens, 300)
	}
	if decoded.CachedCostUSD != 0.003 {
		t.Errorf("CachedCostUSD mismatch: got %f, want %f", decoded.CachedCostUSD, 0.003)
	}
}

func TestMessage_RoleValues(t *testing.T) {
	// Test all valid role values
	roles := []string{"system", "user", "assistant", "tool"}

	for _, role := range roles {
		msg := Message{
			Role:    role,
			Content: "test content",
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("Failed to marshal message with role %q: %v", role, err)
		}

		var decoded Message
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal message with role %q: %v", role, err)
		}

		if decoded.Role != role {
			t.Errorf("Role mismatch: got %q, want %q", decoded.Role, role)
		}
	}
}

// =============================================================================
// Message Constructor Tests
// =============================================================================

func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage("assistant", "Hello, world!")

	if msg.Role != "assistant" {
		t.Errorf("Role mismatch: got %q, want %q", msg.Role, "assistant")
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Content mismatch: got %q, want %q", msg.Content, "Hello, world!")
	}
}

func TestNewUserMessage(t *testing.T) {
	msg := NewUserMessage("What is the weather?")

	if msg.Role != "user" {
		t.Errorf("Role mismatch: got %q, want %q", msg.Role, "user")
	}
	if msg.Content != "What is the weather?" {
		t.Errorf("Content mismatch: got %q, want %q", msg.Content, "What is the weather?")
	}
}

func TestNewAssistantMessage(t *testing.T) {
	msg := NewAssistantMessage("The weather is sunny.")

	if msg.Role != "assistant" {
		t.Errorf("Role mismatch: got %q, want %q", msg.Role, "assistant")
	}
	if msg.Content != "The weather is sunny." {
		t.Errorf("Content mismatch: got %q, want %q", msg.Content, "The weather is sunny.")
	}
}

func TestNewSystemMessage(t *testing.T) {
	msg := NewSystemMessage("You are a helpful assistant.")

	if msg.Role != "system" {
		t.Errorf("Role mismatch: got %q, want %q", msg.Role, "system")
	}
	if msg.Content != "You are a helpful assistant." {
		t.Errorf("Content mismatch: got %q, want %q", msg.Content, "You are a helpful assistant.")
	}
}

func TestNewToolResultMessage(t *testing.T) {
	result := MessageToolResult{
		ID:        "call_123",
		Name:      "get_weather",
		Content:   `{"temp": 72, "condition": "sunny"}`,
		LatencyMs: 150,
	}

	msg := NewToolResultMessage(result)

	if msg.Role != "tool" {
		t.Errorf("Role mismatch: got %q, want %q", msg.Role, "tool")
	}
	if msg.ToolResult == nil {
		t.Fatal("Expected ToolResult to be set")
	}
	if msg.ToolResult.ID != "call_123" {
		t.Errorf("ToolResult.ID mismatch: got %q, want %q", msg.ToolResult.ID, "call_123")
	}
	if msg.ToolResult.Name != "get_weather" {
		t.Errorf("ToolResult.Name mismatch: got %q, want %q", msg.ToolResult.Name, "get_weather")
	}
	// Content should be synced from ToolResult.Content
	if msg.Content != result.Content {
		t.Errorf("Content not synced: got %q, want %q", msg.Content, result.Content)
	}
}

func TestNewToolResultMessage_ContentSync(t *testing.T) {
	// Verify that Content and ToolResult.Content are synchronized
	result := MessageToolResult{
		ID:      "call_456",
		Name:    "calculator",
		Content: "42",
	}

	msg := NewToolResultMessage(result)

	// Both should have the same content
	if msg.Content != msg.ToolResult.Content {
		t.Errorf("Content not synced with ToolResult.Content: Content=%q, ToolResult.Content=%q",
			msg.Content, msg.ToolResult.Content)
	}

	// GetContent should return the content
	if msg.GetContent() != "42" {
		t.Errorf("GetContent() mismatch: got %q, want %q", msg.GetContent(), "42")
	}
}

func TestNewToolResultMessage_WithError(t *testing.T) {
	result := MessageToolResult{
		ID:      "call_789",
		Name:    "invalid_tool",
		Content: "Tool execution failed",
		Error:   "Tool not found",
	}

	msg := NewToolResultMessage(result)

	if msg.ToolResult.Error != "Tool not found" {
		t.Errorf("ToolResult.Error mismatch: got %q, want %q", msg.ToolResult.Error, "Tool not found")
	}
}

func TestNewMultimodalMessage(t *testing.T) {
	text := "Check this image"
	parts := []ContentPart{
		NewTextPart(text),
	}

	msg := NewMultimodalMessage("user", parts)

	if msg.Role != "user" {
		t.Errorf("Role mismatch: got %q, want %q", msg.Role, "user")
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeText {
		t.Errorf("Part type mismatch: got %q, want %q", msg.Parts[0].Type, ContentTypeText)
	}
	// Content should be empty for multimodal messages
	if msg.Content != "" {
		t.Errorf("Content should be empty for multimodal messages, got %q", msg.Content)
	}
	// GetContent should extract text from parts
	if msg.GetContent() != text {
		t.Errorf("GetContent() mismatch: got %q, want %q", msg.GetContent(), text)
	}
}
