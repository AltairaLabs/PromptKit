package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEvalHandler is a minimal handler used to register a working eval type
// in tests without pulling in real handler implementations.
type testEvalHandler struct{}

func (h *testEvalHandler) Type() string { return "test_handler" }

func (h *testEvalHandler) Eval(_ context.Context, _ *evals.EvalContext, _ map[string]any) (*evals.EvalResult, error) {
	return &evals.EvalResult{Passed: true, EvalID: "test", Type: "test_handler"}, nil
}

// newTestRegistry returns a registry with only the testEvalHandler registered.
func newTestRegistry() *evals.EvalTypeRegistry {
	r := evals.NewEmptyEvalTypeRegistry()
	r.Register(&testEvalHandler{})
	return r
}

// sampleDefs returns a slice of EvalDef values useful for testing.
func sampleDefs() []evals.EvalDef {
	return []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
		{ID: "eval-2", Type: "other_handler", Trigger: evals.TriggerOnSessionComplete},
	}
}

func TestNewPackEvalHook_SkipEvalsTrue(t *testing.T) {
	hook := NewPackEvalHook(newTestRegistry(), sampleDefs(), true, nil, "chat")
	// When skipEvals is true, defs are still stored but a NoOpDispatcher is used.
	// HasEvals reflects whether defs exist, not whether the dispatcher is active.
	// RunTurnEvals/RunSessionEvals will still produce nil results via the NoOp path.
	assert.True(t, hook.HasEvals(), "HasEvals reflects stored defs even when skipEvals is true")

	// Verify that the NoOp dispatcher means no actual results are produced.
	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("hi"),
	}
	results := hook.RunTurnEvals(context.Background(), messages, 1, "session-1")
	assert.Empty(t, results, "NoOpDispatcher should produce no results")
}

func TestNewPackEvalHook_EmptyDefs(t *testing.T) {
	hook := NewPackEvalHook(newTestRegistry(), nil, false, nil, "chat")
	assert.False(t, hook.HasEvals(), "HasEvals should be false when defs are empty")

	hook2 := NewPackEvalHook(newTestRegistry(), []evals.EvalDef{}, false, nil, "chat")
	assert.False(t, hook2.HasEvals(), "HasEvals should be false when defs slice is empty")
}

func TestNewPackEvalHook_ValidDefs(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewPackEvalHook(newTestRegistry(), defs, false, nil, "chat")
	assert.True(t, hook.HasEvals(), "HasEvals should be true when valid defs are provided")
}

func TestFilterEvalDefs_EmptyFilter(t *testing.T) {
	defs := sampleDefs()
	result := filterEvalDefs(defs, nil)
	assert.Equal(t, defs, result, "empty filter should return all defs")

	result2 := filterEvalDefs(defs, []string{})
	assert.Equal(t, defs, result2, "empty slice filter should return all defs")
}

func TestFilterEvalDefs_WithFilter(t *testing.T) {
	defs := sampleDefs()
	result := filterEvalDefs(defs, []string{"test_handler"})
	require.Len(t, result, 1)
	assert.Equal(t, "eval-1", result[0].ID)
	assert.Equal(t, "test_handler", result[0].Type)
}

func TestFilterEvalDefs_NoMatch(t *testing.T) {
	defs := sampleDefs()
	result := filterEvalDefs(defs, []string{"nonexistent"})
	assert.Empty(t, result, "filter with no matches should return empty slice")
}

func TestRunTurnEvals_NoEvals(t *testing.T) {
	hook := NewPackEvalHook(newTestRegistry(), nil, false, nil, "chat")
	results := hook.RunTurnEvals(context.Background(), nil, 0, "session-1")
	assert.Nil(t, results, "RunTurnEvals should return nil when there are no evals")
}

func TestRunSessionEvals_NoEvals(t *testing.T) {
	hook := NewPackEvalHook(newTestRegistry(), nil, false, nil, "chat")
	results := hook.RunSessionEvals(context.Background(), nil, "session-1")
	assert.Nil(t, results, "RunSessionEvals should return nil when there are no evals")
}

func TestBuildEvalContext_ExtractsLastAssistantMessage(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewPackEvalHook(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("first reply"),
		types.NewUserMessage("follow up"),
		types.NewAssistantMessage("second reply"),
	}

	evalCtx := hook.buildEvalContext(messages, 3, "session-1")

	assert.Equal(t, "second reply", evalCtx.CurrentOutput, "should extract content from last assistant message")
	assert.Equal(t, 3, evalCtx.TurnIndex)
	assert.Equal(t, "session-1", evalCtx.SessionID)
	assert.Equal(t, "chat", evalCtx.PromptID)
	assert.Len(t, evalCtx.Messages, 4)
}

func TestBuildEvalContext_NoMessages(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewPackEvalHook(newTestRegistry(), defs, false, nil, "chat")

	evalCtx := hook.buildEvalContext(nil, 0, "session-1")

	assert.Empty(t, evalCtx.CurrentOutput, "should have empty current output with no messages")
	assert.Empty(t, evalCtx.ToolCalls, "should have no tool calls with no messages")
	assert.Equal(t, 0, evalCtx.TurnIndex)
	assert.Equal(t, "session-1", evalCtx.SessionID)
}

func TestBuildEvalContext_ExtractsToolCalls(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewPackEvalHook(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("search for cats"),
		{
			Role:    "assistant",
			Content: "I found some results",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search", Args: []byte(`{"q":"cats"}`)},
				{ID: "tc-2", Name: "format", Args: []byte(`{}`)},
			},
		},
	}

	evalCtx := hook.buildEvalContext(messages, 1, "session-1")

	assert.Equal(t, "I found some results", evalCtx.CurrentOutput)
	require.Len(t, evalCtx.ToolCalls, 2)
	assert.Equal(t, "search", evalCtx.ToolCalls[0].ToolName)
	assert.Equal(t, 1, evalCtx.ToolCalls[0].TurnIndex)
	assert.Equal(t, "format", evalCtx.ToolCalls[1].ToolName)
}

func TestRunTurnEvals_WithValidHandler(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewPackEvalHook(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("hi there"),
	}

	results := hook.RunTurnEvals(context.Background(), messages, 1, "session-1")
	require.NotNil(t, results, "should return results when evals are configured")
	assert.Len(t, results, 1)
}

func TestRunSessionEvals_WithValidHandler(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerOnSessionComplete},
	}
	hook := NewPackEvalHook(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("goodbye"),
	}

	results := hook.RunSessionEvals(context.Background(), messages, "session-1")
	require.NotNil(t, results, "should return results when session evals are configured")
	assert.Len(t, results, 1)
}
