package assertions

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// buildTurnMessages constructs a _turn_messages slice for testing.
// Each toolCall has a corresponding result message added automatically.
func buildTurnMessages(toolCalls ...testToolCall) []types.Message {
	var messages []types.Message

	// Group calls into rounds — each round is one assistant message + tool results
	currentRound := -1
	var currentAssistantCalls []types.MessageToolCall
	var pendingResults []testToolCall

	for _, tc := range toolCalls {
		if tc.round != currentRound {
			// Flush previous round
			if len(currentAssistantCalls) > 0 {
				messages = append(messages, types.Message{
					Role:      roleAssistant,
					ToolCalls: currentAssistantCalls,
				})
				for _, pr := range pendingResults {
					messages = append(messages, types.Message{
						Role: "tool",
						ToolResult: &types.MessageToolResult{
							ID:        pr.id,
							Name:      pr.name,
							Content:   pr.result,
							Error:     pr.err,
							LatencyMs: pr.latencyMs,
						},
					})
				}
			}
			currentAssistantCalls = nil
			pendingResults = nil
			currentRound = tc.round
		}

		args, _ := json.Marshal(tc.args)
		currentAssistantCalls = append(currentAssistantCalls, types.MessageToolCall{
			ID:   tc.id,
			Name: tc.name,
			Args: args,
		})
		pendingResults = append(pendingResults, tc)
	}

	// Flush last round
	if len(currentAssistantCalls) > 0 {
		messages = append(messages, types.Message{
			Role:      roleAssistant,
			ToolCalls: currentAssistantCalls,
		})
		for _, pr := range pendingResults {
			messages = append(messages, types.Message{
				Role: "tool",
				ToolResult: &types.MessageToolResult{
					ID:        pr.id,
					Name:      pr.name,
					Content:   pr.result,
					Error:     pr.err,
					LatencyMs: pr.latencyMs,
				},
			})
		}
	}

	return messages
}

// testToolCall is a helper for building test tool calls with results.
type testToolCall struct {
	id        string
	name      string
	args      map[string]interface{}
	result    string
	err       string
	latencyMs int64
	round     int
}

func TestResolveTurnToolTrace_IDMatching(t *testing.T) {
	messages := buildTurnMessages(
		testToolCall{id: "call_1", name: "get_order", args: map[string]interface{}{"id": "123"}, result: `{"status":"shipped"}`, round: 0},
		testToolCall{id: "call_2", name: "get_customer", args: map[string]interface{}{"id": "456"}, result: `{"name":"Alice"}`, round: 0},
	)

	params := map[string]interface{}{"_turn_messages": messages}
	trace, ok := resolveTurnToolTrace(params)

	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(trace) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(trace))
	}

	if trace[0].Name != "get_order" || trace[0].Result != `{"status":"shipped"}` {
		t.Errorf("trace[0] = %+v", trace[0])
	}
	if trace[1].Name != "get_customer" || trace[1].Result != `{"name":"Alice"}` {
		t.Errorf("trace[1] = %+v", trace[1])
	}
	if trace[0].RoundIndex != 0 || trace[1].RoundIndex != 0 {
		t.Errorf("expected round 0 for both, got %d and %d", trace[0].RoundIndex, trace[1].RoundIndex)
	}
}

func TestResolveTurnToolTrace_NameFallback(t *testing.T) {
	// Build messages manually without IDs
	messages := []types.Message{
		{
			Role: roleAssistant,
			ToolCalls: []types.MessageToolCall{
				{Name: "read_file", Args: json.RawMessage(`{"path":"a.txt"}`)},
				{Name: "read_file", Args: json.RawMessage(`{"path":"b.txt"}`)},
			},
		},
		{
			Role:       "tool",
			ToolResult: &types.MessageToolResult{Name: "read_file", Content: "content_a"},
		},
		{
			Role:       "tool",
			ToolResult: &types.MessageToolResult{Name: "read_file", Content: "content_b"},
		},
	}

	params := map[string]interface{}{"_turn_messages": messages}
	trace, ok := resolveTurnToolTrace(params)

	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(trace) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(trace))
	}
	if trace[0].Result != "content_a" {
		t.Errorf("expected first result 'content_a', got %q", trace[0].Result)
	}
	if trace[1].Result != "content_b" {
		t.Errorf("expected second result 'content_b', got %q", trace[1].Result)
	}
}

func TestResolveTurnToolTrace_MultiRound(t *testing.T) {
	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "search", result: "found", round: 0},
		testToolCall{id: "c2", name: "read_file", result: "content", round: 1},
		testToolCall{id: "c3", name: "edit_file", result: "ok", round: 2},
	)

	params := map[string]interface{}{"_turn_messages": messages}
	trace, ok := resolveTurnToolTrace(params)

	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(trace) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(trace))
	}
	for i, expected := range []int{0, 1, 2} {
		if trace[i].RoundIndex != expected {
			t.Errorf("trace[%d].RoundIndex = %d, want %d", i, trace[i].RoundIndex, expected)
		}
	}
}

func TestResolveTurnToolTrace_StatestoreFiltered(t *testing.T) {
	messages := []types.Message{
		{
			Role:   roleAssistant,
			Source: sourceStatestore,
			ToolCalls: []types.MessageToolCall{
				{ID: "old", Name: "old_tool"},
			},
		},
		{
			Role: roleAssistant,
			ToolCalls: []types.MessageToolCall{
				{ID: "new", Name: "new_tool"},
			},
		},
		{
			Role:       "tool",
			ToolResult: &types.MessageToolResult{ID: "new", Name: "new_tool", Content: "result"},
		},
	}

	params := map[string]interface{}{"_turn_messages": messages}
	trace, ok := resolveTurnToolTrace(params)

	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(trace) != 1 {
		t.Fatalf("expected 1 call (statestore filtered), got %d", len(trace))
	}
	if trace[0].Name != "new_tool" {
		t.Errorf("expected new_tool, got %s", trace[0].Name)
	}
}

func TestResolveTurnToolTrace_EmptyResult(t *testing.T) {
	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "notify", result: "", round: 0},
	)

	params := map[string]interface{}{"_turn_messages": messages}
	trace, ok := resolveTurnToolTrace(params)

	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(trace) != 1 {
		t.Fatalf("expected 1 call, got %d", len(trace))
	}
	if !trace[0].resolved {
		t.Error("expected resolved=true for empty-content result")
	}
	if trace[0].Result != "" {
		t.Errorf("expected empty result, got %q", trace[0].Result)
	}
}

func TestResolveTurnToolTrace_NoTurnMessages(t *testing.T) {
	params := map[string]interface{}{}
	trace, ok := resolveTurnToolTrace(params)

	if ok {
		t.Error("expected ok=false when _turn_messages not present")
	}
	if trace != nil {
		t.Error("expected nil trace")
	}
}

func TestResolveTurnToolTrace_EmptyMessages(t *testing.T) {
	params := map[string]interface{}{"_turn_messages": []types.Message{}}
	trace, ok := resolveTurnToolTrace(params)

	if !ok {
		t.Error("expected ok=true for empty messages")
	}
	if len(trace) != 0 {
		t.Errorf("expected 0 calls, got %d", len(trace))
	}
}

func TestResolveTurnToolTrace_ErrorResult(t *testing.T) {
	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "get_order", result: "", err: "not found", round: 0},
	)

	params := map[string]interface{}{"_turn_messages": messages}
	trace, ok := resolveTurnToolTrace(params)

	if !ok {
		t.Fatal("expected ok=true")
	}
	if trace[0].Error != "not found" {
		t.Errorf("expected error 'not found', got %q", trace[0].Error)
	}
}

func TestResolveTurnToolTrace_LatencyMs(t *testing.T) {
	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "slow_tool", result: "done", latencyMs: 1500, round: 0},
	)

	params := map[string]interface{}{"_turn_messages": messages}
	trace, _ := resolveTurnToolTrace(params)

	if trace[0].LatencyMs != 1500 {
		t.Errorf("expected latency 1500, got %d", trace[0].LatencyMs)
	}
}

func TestResolveTurnToolTrace_ArgsPreserved(t *testing.T) {
	messages := buildTurnMessages(
		testToolCall{
			id:     "c1",
			name:   "get_order",
			args:   map[string]interface{}{"order_id": "ORD-123", "verbose": true},
			result: "ok",
			round:  0,
		},
	)

	params := map[string]interface{}{"_turn_messages": messages}
	trace, _ := resolveTurnToolTrace(params)

	if trace[0].Args["order_id"] != "ORD-123" {
		t.Errorf("expected order_id=ORD-123, got %v", trace[0].Args["order_id"])
	}
	if trace[0].RawArgs == nil {
		t.Error("expected RawArgs to be preserved")
	}
}
