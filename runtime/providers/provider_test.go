package providers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// testMockProvider is a minimal mock provider for testing registry functionality
// This is separate from the production mock provider in the mock subpackage
type testMockProvider struct {
	id    string
	value string
}

func (m *testMockProvider) ID() string { return m.id }
func (m *testMockProvider) Predict(ctx context.Context, req PredictionRequest) (PredictionResponse, error) {
	return PredictionResponse{Content: m.value}, nil
}
func (m *testMockProvider) PredictStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}
func (m *testMockProvider) SupportsStreaming() bool      { return false }
func (m *testMockProvider) ShouldIncludeRawOutput() bool { return false }
func (m *testMockProvider) Close() error                 { return nil }
func (m *testMockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}

func TestPredictMessage_Structure(t *testing.T) {
	msg := types.Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", msg.Role)
	}

	if msg.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", msg.Content)
	}
}

func TestPredictMessage_WithToolCalls(t *testing.T) {
	toolCall := types.MessageToolCall{
		Name: "search",
		Args: json.RawMessage(`{"query": "test"}`),
		ID:   "call-123",
	}

	msg := types.Message{
		Role:      "assistant",
		Content:   "Let me search for that",
		ToolCalls: []types.MessageToolCall{toolCall},
	}

	if len(msg.ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(msg.ToolCalls))
	}

	if msg.ToolCalls[0].Name != "search" {
		t.Errorf("Expected tool name 'search', got '%s'", msg.ToolCalls[0].Name)
	}
}

func TestPredictMessage_ToolResult(t *testing.T) {
	msg := types.Message{
		Role:    "tool",
		Content: `{"result": "found"}`,
		ToolResult: &types.MessageToolResult{
			ID:      "call-123",
			Name:    "search",
			Content: `{"result": "found"}`,
		},
	}

	if msg.Role != "tool" {
		t.Error("Expected role 'tool'")
	}

	if msg.ToolResult.ID != "call-123" {
		t.Error("ToolResult.ID mismatch")
	}

	if msg.ToolResult.Name != "search" {
		t.Error("ToolResult.Name mismatch")
	}
}

func TestToolCall_Structure(t *testing.T) {
	args := json.RawMessage(`{"param1": "value1", "param2": 42}`)

	toolCall := types.MessageToolCall{
		Name: "calculate",
		Args: args,
		ID:   "call-456",
	}

	if toolCall.Name != "calculate" {
		t.Error("Name mismatch")
	}

	if toolCall.ID != "call-456" {
		t.Error("ID mismatch")
	}

	// Verify args can be unmarshaled
	var parsed map[string]interface{}
	if err := json.Unmarshal(toolCall.Args, &parsed); err != nil {
		t.Errorf("Failed to unmarshal args: %v", err)
	}

	if parsed["param1"] != "value1" {
		t.Error("Args param1 mismatch")
	}
}

func TestPredictionRequest_Structure(t *testing.T) {
	seed := 42
	req := PredictionRequest{
		System: "You are helpful",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
		Seed:        &seed,
	}

	if req.System != "You are helpful" {
		t.Error("System prompt mismatch")
	}

	if len(req.Messages) != 1 {
		t.Error("Messages count mismatch")
	}

	if req.Temperature != 0.7 {
		t.Error("Temperature mismatch")
	}

	if req.TopP != 0.9 {
		t.Error("TopP mismatch")
	}

	if req.MaxTokens != 1000 {
		t.Error("MaxTokens mismatch")
	}

	if req.Seed == nil || *req.Seed != 42 {
		t.Error("Seed mismatch")
	}
}

func TestPredictionRequest_MultipleMessages(t *testing.T) {
	req := PredictionRequest{
		System: "Test",
		Messages: []types.Message{
			{Role: "user", Content: "First"},
			{Role: "assistant", Content: "Second"},
			{Role: "user", Content: "Third"},
		},
		Temperature: 0.5,
		MaxTokens:   500,
	}

	if len(req.Messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(req.Messages))
	}

	if req.Messages[0].Role != "user" {
		t.Error("First message role mismatch")
	}

	if req.Messages[1].Role != "assistant" {
		t.Error("Second message role mismatch")
	}
}

func TestPredictionResponse_Structure(t *testing.T) {
	resp := PredictionResponse{
		Content: "Response text",
		CostInfo: &types.CostInfo{
			InputTokens:   100,
			OutputTokens:  50,
			CachedTokens:  20,
			InputCostUSD:  0.001,
			OutputCostUSD: 0.0005,
			CachedCostUSD: 0.0001,
			TotalCost:     0.0016,
		},
		Latency: 150 * time.Millisecond,
		Raw:     []byte(`{"data": "raw"}`),
	}

	if resp.Content != "Response text" {
		t.Error("Content mismatch")
	}

	if resp.CostInfo.InputTokens != 100 {
		t.Error("InputTokens mismatch")
	}

	if resp.CostInfo.OutputTokens != 50 {
		t.Error("OutputTokens mismatch")
	}

	if resp.CostInfo.CachedTokens != 20 {
		t.Error("CachedTokens mismatch")
	}

	if resp.Latency != 150*time.Millisecond {
		t.Error("Latency mismatch")
	}
}

func TestPredictionResponse_WithToolCalls(t *testing.T) {
	resp := PredictionResponse{
		Content: "Using tools",
		CostInfo: &types.CostInfo{
			InputTokens:  50,
			OutputTokens: 25,
			TotalCost:    0.001,
		},
		ToolCalls: []types.MessageToolCall{
			{Name: "tool1", Args: json.RawMessage(`{}`), ID: "c1"},
			{Name: "tool2", Args: json.RawMessage(`{}`), ID: "c2"},
		},
	}

	if len(resp.ToolCalls) != 2 {
		t.Errorf("Expected 2 tool calls, got %d", len(resp.ToolCalls))
	}

	if resp.ToolCalls[0].Name != "tool1" {
		t.Error("First tool call name mismatch")
	}

	if resp.ToolCalls[1].Name != "tool2" {
		t.Error("Second tool call name mismatch")
	}
}

func TestCostInfo_Structure(t *testing.T) {
	breakdown := types.CostInfo{
		InputTokens:   1000,
		OutputTokens:  500,
		CachedTokens:  100,
		InputCostUSD:  0.01,
		OutputCostUSD: 0.03,
		CachedCostUSD: 0.001,
		TotalCost:     0.041,
	}

	if breakdown.InputTokens != 1000 {
		t.Error("InputTokens mismatch")
	}

	if breakdown.OutputTokens != 500 {
		t.Error("OutputTokens mismatch")
	}

	if breakdown.CachedTokens != 100 {
		t.Error("CachedTokens mismatch")
	}

	if breakdown.TotalCost != 0.041 {
		t.Error("TotalCost mismatch")
	}
}

func TestCostInfo_ZeroCosts(t *testing.T) {
	breakdown := types.CostInfo{
		InputTokens:  0,
		OutputTokens: 0,
		TotalCost:    0.0,
	}

	if breakdown.TotalCost != 0.0 {
		t.Error("Expected zero total cost")
	}
}

func TestPricing_Structure(t *testing.T) {
	pricing := Pricing{
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
	}

	if pricing.InputCostPer1K != 0.01 {
		t.Error("InputCostPer1K mismatch")
	}

	if pricing.OutputCostPer1K != 0.03 {
		t.Error("OutputCostPer1K mismatch")
	}
}

func TestToolDescriptor_Structure(t *testing.T) {
	inputSchema := json.RawMessage(`{"type": "object", "properties": {"query": {"type": "string"}}}`)
	outputSchema := json.RawMessage(`{"type": "object"}`)

	descriptor := ToolDescriptor{
		Name:         "search",
		Description:  "Search for information",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
	}

	if descriptor.Name != "search" {
		t.Error("Name mismatch")
	}

	if descriptor.Description != "Search for information" {
		t.Error("Description mismatch")
	}

	// Verify schemas can be unmarshaled
	var inputParsed map[string]interface{}
	if err := json.Unmarshal(descriptor.InputSchema, &inputParsed); err != nil {
		t.Errorf("Failed to unmarshal input schema: %v", err)
	}
}

func TestToolResult_Structure(t *testing.T) {
	resultContent := `{"results": ["item1", "item2"]}`
	result := ToolResult{
		Name:      "search",
		ID:        "call-789",
		Content:   resultContent,
		LatencyMs: 250,
		Error:     "",
	}

	if result.Name != "search" {
		t.Error("Name mismatch")
	}

	if result.ID != "call-789" {
		t.Error("ID mismatch")
	}

	if result.LatencyMs != 250 {
		t.Error("LatencyMs mismatch")
	}

	if result.Error != "" {
		t.Error("Expected empty error")
	}
}

func TestToolResult_WithError(t *testing.T) {
	result := ToolResult{
		Name:      "failing_tool",
		ID:        "call-error",
		Content:   "",
		LatencyMs: 100,
		Error:     "tool execution failed",
	}

	if result.Error == "" {
		t.Error("Expected error message")
	}

	if result.Error != "tool execution failed" {
		t.Errorf("Error message mismatch: %s", result.Error)
	}
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("Expected non-nil registry")
	}

	if registry.providers == nil {
		t.Error("Expected providers map to be initialized")
	}
}

func TestRegistry_Register(t *testing.T) {
	registry := NewRegistry()
	provider := &testMockProvider{id: "test-provider"}

	registry.Register(provider)

	// Verify provider was registered
	retrieved, exists := registry.Get("test-provider")
	if !exists {
		t.Error("Provider should exist after registration")
	}

	if retrieved.ID() != "test-provider" {
		t.Error("Retrieved provider ID mismatch")
	}
}

func TestRegistry_Get_Existing(t *testing.T) {
	registry := NewRegistry()
	provider := &testMockProvider{id: "existing"}

	registry.Register(provider)

	retrieved, exists := registry.Get("existing")

	if !exists {
		t.Error("Provider should exist")
	}

	if retrieved == nil {
		t.Error("Retrieved provider should not be nil")
	}

	if retrieved.ID() != "existing" {
		t.Error("Provider ID mismatch")
	}
}

func TestRegistry_Get_NonExisting(t *testing.T) {
	registry := NewRegistry()

	retrieved, exists := registry.Get("nonexistent")

	if exists {
		t.Error("Provider should not exist")
	}

	if retrieved != nil {
		t.Error("Retrieved provider should be nil for nonexistent ID")
	}
}

func TestRegistry_List_Empty(t *testing.T) {
	registry := NewRegistry()

	ids := registry.List()

	if len(ids) != 0 {
		t.Errorf("Expected 0 providers, got %d", len(ids))
	}
}

func TestRegistry_List_Multiple(t *testing.T) {
	registry := NewRegistry()

	providers := []*testMockProvider{
		{id: "provider1"},
		{id: "provider2"},
		{id: "provider3"},
	}

	for _, p := range providers {
		registry.Register(p)
	}

	ids := registry.List()

	if len(ids) != 3 {
		t.Errorf("Expected 3 providers, got %d", len(ids))
	}

	// Verify all IDs are present
	idMap := make(map[string]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	for _, p := range providers {
		if !idMap[p.ID()] {
			t.Errorf("Provider %s not found in list", p.ID())
		}
	}
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	registry := NewRegistry()

	provider1 := &testMockProvider{id: "test", value: "first"}
	provider2 := &testMockProvider{id: "test", value: "second"}

	registry.Register(provider1)
	registry.Register(provider2) // Should overwrite

	retrieved, exists := registry.Get("test")

	if !exists {
		t.Error("Provider should exist")
	}

	if mock, ok := retrieved.(*testMockProvider); ok {
		if mock.value != "second" {
			t.Error("Provider should be overwritten with second value")
		}
	}
}

func TestRegistry_Close(t *testing.T) {
	registry := NewRegistry()

	provider1 := &testMockProvider{id: "provider1", value: "first"}
	provider2 := &testMockProvider{id: "provider2", value: "second"}

	registry.Register(provider1)
	registry.Register(provider2)

	// Close should not error
	err := registry.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Should be safe to call Close() multiple times
	err = registry.Close()
	if err != nil {
		t.Errorf("Second Close() returned error: %v", err)
	}
}

func TestPredictMessage_DifferentRoles(t *testing.T) {
	roles := []string{"user", "assistant", "system", "tool"}

	for _, role := range roles {
		msg := types.Message{
			Role:    role,
			Content: "Test content",
		}

		if msg.Role != role {
			t.Errorf("Role mismatch for %s", role)
		}
	}
}

func TestPredictionRequest_NilSeed(t *testing.T) {
	req := PredictionRequest{
		System:      "Test",
		Messages:    []types.Message{{Role: "user", Content: "Hi"}},
		Temperature: 0.7,
		MaxTokens:   100,
		Seed:        nil, // Explicitly nil
	}

	if req.Seed != nil {
		t.Error("Seed should be nil")
	}
}

func TestPredictionResponse_ZeroLatency(t *testing.T) {
	resp := PredictionResponse{
		Content: "Fast response",
		CostInfo: &types.CostInfo{
			InputTokens:  10,
			OutputTokens: 10,
			TotalCost:    0.001,
		},
		Latency: 0,
	}

	if resp.Latency != 0 {
		t.Error("Expected zero latency")
	}
}

func TestToolCall_EmptyArgs(t *testing.T) {
	toolCall := types.MessageToolCall{
		Name: "no_args_tool",
		Args: json.RawMessage(`{}`),
		ID:   "call-empty",
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(toolCall.Args, &parsed); err != nil {
		t.Errorf("Failed to unmarshal empty args: %v", err)
	}

	if len(parsed) != 0 {
		t.Error("Expected empty args object")
	}
}
