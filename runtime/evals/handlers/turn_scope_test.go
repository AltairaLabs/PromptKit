package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// twoTurnsEarlierCall is a conversation whose first turn called lookup and
// whose second, current, turn called nothing.
func twoTurnsEarlierCall() *evals.EvalContext {
	return evals.BuildEvalContext([]types.Message{
		{Role: "user", Content: "find x"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup"}}},
		{Role: "assistant", Content: "found"},
		{Role: "user", Content: "thanks"},
		{Role: "assistant", Content: "welcome"},
	}, 1, "s1", "p1", nil)
}

// The checks that judge one turn are given only the current turn's calls; the
// session-level checks keep every call (#2927). Which side of the line a type
// is on is what a pack author sees, so each built-in tool check is pinned.
func TestTurnScope_BuiltInToolChecks(t *testing.T) {
	turnLevel := []string{
		"tools_called", "tools_not_called", "tools_offered", "tool_args",
		"tool_call_count", "tool_calls_with_args", "tool_result_includes",
		"tool_result_matches", "tool_result_has_media", "tool_result_media_type",
		"no_tool_errors",
	}
	sessionLevel := []string{
		"tools_called_session", "tools_not_called_session", "tool_args_session",
		"tool_args_excluded_session", "tool_call_sequence", "tool_call_chain",
		"tool_anti_pattern", "tool_no_repeat", "tool_efficiency",
		"agent_invoked", "skill_activated", "workflow_tool_access",
		"llm_judge_tool_calls", "invariant_fields_preserved",
	}
	registry := evals.NewEvalTypeRegistry()
	visibleTools := func(t *testing.T, evalType string) []string {
		t.Helper()
		handler, err := registry.Get(evalType)
		require.NoError(t, err)
		names := []string{}
		for _, tc := range evals.ScopeToTurn(handler, nil, twoTurnsEarlierCall()).ToolCalls {
			names = append(names, tc.ToolName)
		}
		return names
	}

	for _, evalType := range turnLevel {
		assert.Equal(t, []string{}, visibleTools(t, evalType), "%s must not see turn 1's call", evalType)
	}
	for _, evalType := range sessionLevel {
		assert.Equal(t, []string{"lookup"}, visibleTools(t, evalType), "%s must see turn 1's call", evalType)
	}
}
