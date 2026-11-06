package stage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// runCallsTurn runs one tool round of a turn of conversation convID on stage,
// calling each named tool once, and returns the results by call ID.
func runCallsTurn(
	t *testing.T, stage *ProviderStage, convID string, calls ...types.MessageToolCall,
) map[string]*types.MessageToolResult {
	t.Helper()
	stage.turnState.ConversationID = convID
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: []string{"read_kit", "list_kits"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	response := types.Message{Role: "assistant", ToolCalls: calls}
	_, _, err = loop.afterRound(context.Background(),
		[]string{"read_kit", "list_kits"}, &response, true, roundRef{round: 1})
	require.NoError(t, err)
	return toolResultsByID(loop.messages)
}

func kitCall(id, name string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: name, Args: json.RawMessage(`{"id":"` + id + `"}`)}
}

func TestProviderStage_MaxCallsPerMinute(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit", "list_kits")
	clock := newFakeClock()
	stage.callRate.now = clock.now

	// Turn 1: two calls to read_kit fill its window.
	res := runCallsTurn(t, stage, "conv-a", kitCall("a1", "read_kit"), kitCall("a2", "read_kit"))
	assert.Empty(t, res["a1"].Error)
	assert.Empty(t, res["a2"].Error)

	// Turn 2, 30s later, same conversation: the window spans turns, so a third
	// call is refused; another tool has its own window.
	clock.t = clock.t.Add(30 * time.Second)
	res = runCallsTurn(t, stage, "conv-a", kitCall("b1", "read_kit"), kitCall("b2", "list_kits"))
	require.Contains(t, res, "b1", "a refused call still gets a result")
	assert.Equal(t, "Tool read_kit not executed: its limit of 2 calls per minute is reached", res["b1"].Error)
	assert.Equal(t, res["b1"].Error, res["b1"].GetTextContent(), "the model is told why")
	assert.Equal(t, types.ToolErrorApproval, res["b1"].ErrorType)
	assert.Empty(t, res["b2"].Error, "list_kits has its own window")

	// Another conversation on the same stage has its own window.
	res = runCallsTurn(t, stage, "conv-b", kitCall("c1", "read_kit"))
	assert.Empty(t, res["c1"].Error, "the limit is per conversation")

	// Once a minute has passed since the first call, read_kit runs again.
	clock.t = clock.t.Add(31 * time.Second)
	res = runCallsTurn(t, stage, "conv-a", kitCall("d1", "read_kit"))
	assert.Empty(t, res["d1"].Error, "the window slides")
}

func TestProviderStage_MaxCallsPerMinuteUnsetIsUnlimited(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{}, nil, "read_kit", "list_kits")
	calls := make([]types.MessageToolCall, 0, 5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		calls = append(calls, kitCall(id, "read_kit"))
	}
	res := runCallsTurn(t, stage, "conv-a", calls...)
	for id, r := range res {
		assert.Empty(t, r.Error, "call %s", id)
	}
}

func TestCallRateWindow_DropsIdleConversations(t *testing.T) {
	clock := newFakeClock()
	w := NewCallRateWindow()
	w.now = clock.now
	require.True(t, w.allow("conv-a", "read_kit", 1))
	require.False(t, w.allow("conv-a", "read_kit", 1))

	clock.t = clock.t.Add(2 * time.Minute)
	require.True(t, w.allow("conv-b", "read_kit", 1))
	assert.Len(t, w.calls, 1, "an expired window is dropped, not kept forever")
}

// A call held for approval has not run, so it does not count against
// MaxCallsPerMinute when it is held: two held calls that a human then rejects
// must not refuse a third call the same minute.
func TestProviderStage_MaxCallsPerMinuteSkipsHeldCalls(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 1}, nil, "read_kit")
	stage.callRate.now = newFakeClock().now
	stage.turnState.ConversationID = "conv-a"
	hold := true
	stage.config.ApprovalChecker = func(_ context.Context, _, name string, _ map[string]any) *tools.PendingToolInfo {
		if !hold {
			return nil
		}
		return &tools.PendingToolInfo{Reason: "requires_approval", ToolName: name}
	}

	for _, id := range []string{"h1", "h2"} {
		_, err := stage.executeToolCalls(context.Background(),
			[]types.MessageToolCall{kitCall(id, "read_kit")}, roundRef{round: 1})
		var pending *tools.ErrToolsPending
		require.ErrorAs(t, err, &pending, "call %s is held", id)
	}

	hold = false
	results, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{kitCall("r1", "read_kit")}, roundRef{round: 1})
	require.NoError(t, err)
	res := toolResultsByID(results)
	require.Contains(t, res, "r1")
	assert.Empty(t, res["r1"].Error, "held calls that never ran must not use up the limit")

	// The call that did run counts.
	results, err = stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{kitCall("r2", "read_kit")}, roundRef{round: 1})
	require.NoError(t, err)
	assert.Equal(t, "Tool read_kit not executed: its limit of 1 calls per minute is reached",
		toolResultsByID(results)["r2"].Error)
}

// A window shared through ProviderConfig.CallRate is the one the stage
// counts in, and a call admitted there from outside the stage — a held call a
// human approved — uses up the limit like a call the stage ran.
func TestProviderStage_MaxCallsPerMinuteCountsAdmittedCalls(t *testing.T) {
	window := NewCallRateWindow()
	window.now = newFakeClock().now
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit", "list_kits")
	stage.config.CallRate = window
	stage.callRate = window // as NewProviderStageWithTurnState wires it from the config

	window.Admit("conv-a", "read_kit")
	res := runCallsTurn(t, stage, "conv-a", kitCall("a1", "read_kit"), kitCall("a2", "read_kit"))
	errs := []string{res["a1"].Error, res["a2"].Error}
	assert.ElementsMatch(t, []string{"", "Tool read_kit not executed: its limit of 2 calls per minute is reached"}, errs,
		"the admitted call takes one of the two slots")

	var nilWindow *CallRateWindow
	assert.NotPanics(t, func() { nilWindow.Admit("conv-a", "read_kit") })
}

func TestNewProviderStage_UsesConfiguredCallRate(t *testing.T) {
	window := NewCallRateWindow()
	stage := NewProviderStageWithTurnState(nil, nil, nil, &ProviderConfig{CallRate: window}, nil, nil, nil)
	assert.Same(t, window, stage.callRate)
	assert.NotNil(t, NewProviderStageWithTurnState(nil, nil, nil, nil, nil, nil, nil).callRate,
		"without one the stage has its own")
}
