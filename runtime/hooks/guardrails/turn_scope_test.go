package guardrails

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// earlierTurnCallingLookup is a finished turn whose model called lookup,
// followed by the user message that opens the next turn.
func earlierTurnCallingLookup() []types.Message {
	return []types.Message{
		{Role: "user", Content: "find x"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "lookup", Args: json.RawMessage(`{"q":"x"}`)},
		}},
		toolResultMessage("c1", "lookup", "found x"),
		{Role: "assistant", Content: "x is found"},
		{Role: "user", Content: "thanks"},
	}
}

// A guardrail judges the turn in progress, so a turn-level tool check must not
// fire on a call an earlier turn made (#2927). Without that, one lookup call
// would block every later reply in the conversation.
func TestGuardrailTurnScope_ToolsNotCalledIgnoresEarlierTurns(t *testing.T) {
	hook, err := NewGuardrailHook("tools_not_called", map[string]any{
		"tool_names": []any{"lookup"},
		"direction":  "output",
	})
	require.NoError(t, err)
	req := &hooks.ProviderRequest{Messages: earlierTurnCallingLookup()}

	d := hook.AfterCall(context.Background(), req, &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "you're welcome"},
	})
	assert.True(t, d.Allow, "this turn made no lookup call")

	d = hook.AfterCall(context.Background(), req, &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "c2", Name: "lookup", Args: json.RawMessage(`{"q":"y"}`)},
		}},
	})
	assert.False(t, d.Allow, "a lookup call in this turn still fires the guardrail")
}

// A session-level check used as a guardrail still sees the whole history.
func TestGuardrailTurnScope_SessionLevelCheckSeesEarlierTurns(t *testing.T) {
	hook, err := NewGuardrailHook("tools_not_called_session", map[string]any{
		"tools":     []any{"lookup"},
		"direction": "output",
	})
	require.NoError(t, err)
	req := &hooks.ProviderRequest{Messages: earlierTurnCallingLookup()}

	d := hook.AfterCall(context.Background(), req, &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "you're welcome"},
	})
	assert.False(t, d.Allow, "the session called lookup on an earlier turn")
}
