package assertions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBuildConversationContextFromMessages_Basic(t *testing.T) {
	messages := []types.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
		{
			Role:    "assistant",
			Content: "Let me look that up.",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc1", Name: "search", Args: json.RawMessage(`{"query":"hello"}`)},
			},
		},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	if len(ctx.AllTurns) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(ctx.AllTurns))
	}
	if len(ctx.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(ctx.ToolCalls))
	}

	tc := ctx.ToolCalls[0]
	if tc.ToolName != "search" {
		t.Errorf("expected tool name 'search', got %q", tc.ToolName)
	}
	if tc.TurnIndex != 2 {
		t.Errorf("expected turn index 2, got %d", tc.TurnIndex)
	}
	if tc.Arguments["query"] != "hello" {
		t.Errorf("expected query='hello', got %v", tc.Arguments["query"])
	}
}

func TestBuildConversationContextFromMessages_ToolResults(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Search for something"},
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc1", Name: "search", Args: json.RawMessage(`{"q":"test"}`)},
			},
		},
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID:        "tc1",
				Name:      "search",
				Content:   "found 3 results",
				Error:     "",
				LatencyMs: 150,
			},
		},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	if len(ctx.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(ctx.ToolCalls))
	}

	tc := ctx.ToolCalls[0]
	if tc.Result != "found 3 results" {
		t.Errorf("expected result 'found 3 results', got %v", tc.Result)
	}
	if tc.Error != "" {
		t.Errorf("expected no error, got %q", tc.Error)
	}
	if tc.Duration != 150*time.Millisecond {
		t.Errorf("expected 150ms duration, got %v", tc.Duration)
	}
}

func TestBuildConversationContextFromMessages_ToolResultWithError(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc1", Name: "db_query", Args: json.RawMessage(`{}`)},
			},
		},
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID:        "tc1",
				Name:      "db_query",
				Content:   "",
				Error:     "connection refused",
				LatencyMs: 50,
			},
		},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	tc := ctx.ToolCalls[0]
	if tc.Error != "connection refused" {
		t.Errorf("expected error 'connection refused', got %q", tc.Error)
	}
	if tc.Duration != 50*time.Millisecond {
		t.Errorf("expected 50ms duration, got %v", tc.Duration)
	}
}

func TestBuildConversationContextFromMessages_MultipleToolCalls(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc1", Name: "search", Args: json.RawMessage(`{"q":"a"}`)},
				{ID: "tc2", Name: "lookup", Args: json.RawMessage(`{"id":"123"}`)},
			},
		},
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID: "tc1", Name: "search", Content: "result-a", LatencyMs: 10,
			},
		},
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID: "tc2", Name: "lookup", Content: "result-b", LatencyMs: 20,
			},
		},
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc3", Name: "search", Args: json.RawMessage(`{"q":"b"}`)},
			},
		},
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID: "tc3", Name: "search", Content: "result-c", LatencyMs: 30,
			},
		},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	if len(ctx.ToolCalls) != 3 {
		t.Fatalf("expected 3 tool calls, got %d", len(ctx.ToolCalls))
	}

	// First search call
	if ctx.ToolCalls[0].Result != "result-a" {
		t.Errorf("tc[0] result: expected 'result-a', got %v", ctx.ToolCalls[0].Result)
	}
	// Lookup call
	if ctx.ToolCalls[1].Result != "result-b" {
		t.Errorf("tc[1] result: expected 'result-b', got %v", ctx.ToolCalls[1].Result)
	}
	// Second search call
	if ctx.ToolCalls[2].Result != "result-c" {
		t.Errorf("tc[2] result: expected 'result-c', got %v", ctx.ToolCalls[2].Result)
	}
}

func TestBuildConversationContextFromMessages_WorkflowMetadata(t *testing.T) {
	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "Transitioning...",
			Meta: map[string]interface{}{
				"_workflow_state":       "greeting",
				"_workflow_transitions": []string{"init->greeting"},
			},
		},
		{
			Role:    "assistant",
			Content: "Done.",
			Meta: map[string]interface{}{
				"_workflow_state":    "complete",
				"_workflow_complete": true,
			},
		},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	if ctx.Metadata.Extras["_workflow_state"] != "complete" {
		t.Errorf("expected workflow state 'complete', got %v", ctx.Metadata.Extras["_workflow_state"])
	}
	if ctx.Metadata.Extras["_workflow_complete"] != true {
		t.Errorf("expected workflow complete true, got %v", ctx.Metadata.Extras["_workflow_complete"])
	}
}

func TestBuildConversationContextFromMessages_CostAggregation(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			CostInfo: &types.CostInfo{
				InputTokens:  100,
				OutputTokens: 50,
				TotalCost:    0.01,
			},
		},
		{
			Role: "assistant",
			CostInfo: &types.CostInfo{
				InputTokens:  200,
				OutputTokens: 80,
				TotalCost:    0.02,
			},
		},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	const epsilon = 0.0001
	if ctx.Metadata.TotalCost < 0.03-epsilon || ctx.Metadata.TotalCost > 0.03+epsilon {
		t.Errorf("expected total cost ~0.03, got %f", ctx.Metadata.TotalCost)
	}
	if ctx.Metadata.TotalTokens != 430 {
		t.Errorf("expected 430 total tokens, got %d", ctx.Metadata.TotalTokens)
	}
}

func TestBuildConversationContextFromMessages_EmptyMessages(t *testing.T) {
	ctx := BuildConversationContextFromMessages(nil, &ConversationMetadata{})

	if ctx.AllTurns != nil {
		t.Errorf("expected nil AllTurns, got %v", ctx.AllTurns)
	}
	if len(ctx.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(ctx.ToolCalls))
	}
	if ctx.Metadata.Extras == nil {
		t.Error("expected non-nil Extras map")
	}
}

func TestBuildConversationContextFromMessages_NoToolCalls(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello!"},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	if len(ctx.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(ctx.ToolCalls))
	}
}

func TestBuildConversationContextFromMessages_ExtrasPreserved(t *testing.T) {
	existingExtras := map[string]interface{}{
		"judge_targets": []string{"accuracy"},
		"custom_key":    42,
	}

	ctx := BuildConversationContextFromMessages(
		[]types.Message{{Role: "user", Content: "test"}},
		&ConversationMetadata{
			ScenarioID: "s1",
			Extras:     existingExtras,
		},
	)

	if ctx.Metadata.ScenarioID != "s1" {
		t.Errorf("expected scenario ID 's1', got %q", ctx.Metadata.ScenarioID)
	}
	if ctx.Metadata.Extras["custom_key"] != 42 {
		t.Errorf("expected custom_key=42, got %v", ctx.Metadata.Extras["custom_key"])
	}
	if ctx.Metadata.Extras["judge_targets"] == nil {
		t.Error("expected judge_targets to be preserved")
	}
}

func TestBuildConversationContextFromMessages_InvalidToolArgs(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc1", Name: "broken", Args: json.RawMessage(`not-json`)},
			},
		},
	}

	ctx := BuildConversationContextFromMessages(messages, &ConversationMetadata{})

	if len(ctx.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(ctx.ToolCalls))
	}
	if ctx.ToolCalls[0].Arguments != nil {
		t.Errorf("expected nil arguments for invalid JSON, got %v", ctx.ToolCalls[0].Arguments)
	}
}
