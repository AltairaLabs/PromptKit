package stage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
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
	stage.callRate.counts.now = clock.now

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
	w.counts.now = clock.now
	_, ok := w.reserve("conv-a", "read_kit", "a1", 1)
	require.True(t, ok)
	_, ok = w.reserve("conv-a", "read_kit", "a2", 1)
	require.False(t, ok)
	w.Commit("conv-a", "read_kit", "a1")
	w.Release("conv-b", "read_kit", "b0") // resolving an unknown call keeps no window

	clock.t = clock.t.Add(2 * time.Minute)
	_, ok = w.reserve("conv-b", "read_kit", "b1", 1)
	require.True(t, ok)
	assert.Len(t, w.counts.slots, 1, "an expired window is dropped, not kept forever")
}

// A reservation the host never resolves lapses on the sweep too, so a held
// call abandoned in an idle conversation does not keep its window forever.
func TestCallRateWindow_SweepDropsLapsedReservations(t *testing.T) {
	clock := newFakeClock()
	w := NewCallRateWindow()
	w.counts.now = clock.now
	_, ok := w.reserve("conv-a", "read_kit", "a1", 1)
	require.True(t, ok)

	clock.t = clock.t.Add(CallRateReservationLifetime + time.Second)
	_, ok = w.reserve("conv-b", "read_kit", "b1", 1)
	require.True(t, ok)
	assert.Len(t, w.counts.slots, 1, "the lapsed reservation's window is dropped")
}

// A window shared through ProviderConfig.CallRate is the one the stage
// counts in, and a call committed there from outside the stage (a held call a
// human approved) uses up the limit like a call the stage ran.
func TestProviderStage_MaxCallsPerMinuteCountsCommittedCalls(t *testing.T) {
	window := NewCallRateWindow()
	window.counts.now = newFakeClock().now
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit", "list_kits")
	stage.config.CallRate = window
	stage.callRate = window // as NewProviderStageWithTurnState wires it from the config

	window.Commit("conv-a", "read_kit", "approved-elsewhere")
	res := runCallsTurn(t, stage, "conv-a", kitCall("a1", "read_kit"), kitCall("a2", "read_kit"))
	errs := []string{res["a1"].Error, res["a2"].Error}
	assert.ElementsMatch(t, []string{"", "Tool read_kit not executed: its limit of 2 calls per minute is reached"}, errs,
		"the committed call takes one of the two slots")

	var nilWindow *CallRateWindow
	assert.NotPanics(t, func() {
		nilWindow.Commit("conv-a", "read_kit", "x")
		nilWindow.Release("conv-a", "read_kit", "x")
	})
	assert.Nil(t, nilWindow.Scoped("wf"))
}

// A scoped view counts every conversation's calls under its scope, in the
// counts of the window it views.
func TestCallRateWindow_ScopedSharesOneLimit(t *testing.T) {
	window := NewCallRateWindow()
	window.counts.now = newFakeClock().now
	scoped := window.Scoped("wf")

	_, ok := scoped.reserve("conv-a", "read_kit", "a1", 2)
	require.True(t, ok)
	scoped.Commit("conv-b", "read_kit", "b1")
	_, ok = scoped.reserve("conv-c", "read_kit", "c1", 2)
	assert.False(t, ok, "the three conversations share the scope's limit")

	scoped.Release("conv-z", "read_kit", "a1")
	_, ok = scoped.reserve("conv-c", "read_kit", "c1", 2)
	assert.True(t, ok, "a release under any conversation frees the scope's slot")

	_, ok = window.reserve("conv-a", "read_kit", "u1", 1)
	assert.True(t, ok, "the unscoped window keys on the conversation as before")
}

func TestNewProviderStage_UsesConfiguredCallRate(t *testing.T) {
	window := NewCallRateWindow()
	stage := NewProviderStageWithTurnState(nil, nil, nil, &ProviderConfig{CallRate: window}, nil, nil, nil)
	assert.Same(t, window, stage.callRate)
	assert.NotNil(t, NewProviderStageWithTurnState(nil, nil, nil, nil, nil, nil, nil).callRate,
		"without one the stage has its own")
}
