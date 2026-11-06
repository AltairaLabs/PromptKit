package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const readKitLimitReached = "Tool read_kit not executed: its limit of 2 calls per minute is reached"

// kitCalls returns n calls to read_kit, with ids prefix0..prefix(n-1).
func kitCalls(prefix string, n int) []types.MessageToolCall {
	calls := make([]types.MessageToolCall, n)
	for i := range calls {
		calls[i] = kitCall(fmt.Sprintf("%s%d", prefix, i), "read_kit")
	}
	return calls
}

// holdAll makes stage hold every call for approval, counting how many calls
// reached the approval gate.
func holdAll(stage *ProviderStage) *atomic.Int32 {
	checked := &atomic.Int32{}
	stage.config.ApprovalChecker = func(_ context.Context, _, name string, _ map[string]any) *tools.PendingToolInfo {
		checked.Add(1)
		return &tools.PendingToolInfo{Reason: "requires_approval", ToolName: name}
	}
	return checked
}

// countLimitRefusals counts the results refused by a limit of 2 on read_kit.
func countLimitRefusals(results []types.Message) int {
	n := 0
	for _, r := range toolResultsByID(results) {
		if r.Error == readKitLimitReached {
			n++
		}
	}
	return n
}

// A call held for approval reserves a slot when the model makes it: with a
// limit of 2, twenty approval-gated calls in one minute leave two held and
// refuse the rest at once, before the approval gate sees them.
func TestProviderStage_MaxCallsPerMinuteReservesHeldCalls(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit")
	stage.callRate.counts.now = newFakeClock().now
	stage.turnState.ConversationID = "conv-a"
	checked := holdAll(stage)

	results, err := stage.executeToolCalls(context.Background(), kitCalls("h", 20), roundRef{round: 1})
	var pending *tools.ErrToolsPending
	require.ErrorAs(t, err, &pending)
	assert.Len(t, pending.Pending, 2, "two calls are held")
	assert.Equal(t, 18, countLimitRefusals(results), "the rest are refused")
	assert.EqualValues(t, 2, checked.Load(), "a refused call never reaches the approval gate")
}

// A rejected held call gives its slot back; one that is never resolved keeps
// it until its reservation lapses.
func TestProviderStage_MaxCallsPerMinuteReleaseFreesSlot(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit")
	clock := newFakeClock()
	stage.callRate.counts.now = clock.now
	stage.turnState.ConversationID = "conv-a"
	holdAll(stage)

	_, err := stage.executeToolCalls(context.Background(), kitCalls("h", 2), roundRef{round: 1})
	require.Error(t, err)

	stage.config.ApprovalChecker = nil
	results, err := stage.executeToolCalls(context.Background(), kitCalls("r", 1), roundRef{round: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, countLimitRefusals(results), "both slots are reserved by the held calls")

	stage.callRate.Release("conv-a", "read_kit", "h0")
	results, err = stage.executeToolCalls(context.Background(), kitCalls("s", 2), roundRef{round: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, countLimitRefusals(results), "the rejection freed exactly one slot")

	// The other held call is never resolved: its slot outlives the minute that
	// frees committed calls, and lapses with its reservation.
	clock.t = clock.t.Add(callRateWindowLength + 1)
	results, err = stage.executeToolCalls(context.Background(), kitCalls("t", 2), roundRef{round: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, countLimitRefusals(results), "an unresolved held call still holds its slot")

	clock.t = clock.t.Add(CallRateReservationLifetime)
	results, err = stage.executeToolCalls(context.Background(), kitCalls("u", 2), roundRef{round: 1})
	require.NoError(t, err)
	assert.Zero(t, countLimitRefusals(results), "the reservation lapsed and the committed calls aged out")
}

// deferringExecutor defers every call, as a client tool does: the stage gets
// ToolStatusPending back and the call runs, or not, elsewhere.
type deferringExecutor struct{}

func (deferringExecutor) Name() string { return "deferring" }

func (deferringExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("deferringExecutor runs asynchronously only")
}

func (deferringExecutor) ExecuteAsync(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	return &tools.ToolExecutionResult{
		Status:      tools.ToolStatusPending,
		PendingInfo: &tools.PendingToolInfo{Reason: "client_tool_deferred", ToolName: d.Name},
	}, nil
}

// A call the executor defers reserves its slot rather than committing it, so
// a rejection gives it back and a result commits it.
func TestProviderStage_MaxCallsPerMinuteExecutorDeferredCalls(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil)
	clock := newFakeClock()
	stage.callRate.counts.now = clock.now
	stage.turnState.ConversationID = "conv-a"
	stage.toolRegistry.RegisterExecutor(deferringExecutor{})
	require.NoError(t, stage.toolRegistry.Register(&tools.ToolDescriptor{
		Name: "read_kit", Description: "test", InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "deferring",
	}))

	results, err := stage.executeToolCalls(context.Background(), kitCalls("d", 3), roundRef{round: 1})
	var pending *tools.ErrToolsPending
	require.ErrorAs(t, err, &pending)
	assert.Len(t, pending.Pending, 2)
	assert.Equal(t, 1, countLimitRefusals(results), "two deferred calls reserve both slots")

	// The round's calls run in parallel, so which two were deferred varies.
	stage.callRate.Release("conv-a", "read_kit", pending.Pending[0].CallID) // the client rejected it
	stage.callRate.Commit("conv-a", "read_kit", pending.Pending[1].CallID)  // the client answered it
	results, err = stage.executeToolCalls(context.Background(), kitCalls("e", 2), roundRef{round: 1})
	require.ErrorAs(t, err, &pending)
	assert.Len(t, pending.Pending, 1, "the rejected call's slot is free again")
	assert.Equal(t, 1, countLimitRefusals(results), "the answered call keeps its slot")

	// A committed slot ages out with the window; the e call's reservation holds.
	clock.t = clock.t.Add(callRateWindowLength + 1)
	results, err = stage.executeToolCalls(context.Background(), kitCalls("f", 2), roundRef{round: 1})
	require.ErrorAs(t, err, &pending)
	assert.Len(t, pending.Pending, 1, "the committed slot aged out")
	assert.Equal(t, 1, countLimitRefusals(results), "the unresolved reservation is still held")
}

// countingEcho is a local executor that counts its runs.
type countingEcho struct{ runs atomic.Int32 }

func (e *countingEcho) Name() string { return "counting-echo" }

func (e *countingEcho) Execute(_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	e.runs.Add(1)
	return args, nil
}

// A composition step's calls count under the enclosing conversation's ID, so
// a window shared with the conversation's own turns spans both.
func TestCompositionExecutor_StepCallsCountUnderTheConversation(t *testing.T) {
	window := NewCallRateWindow()
	policy := &pipeline.ToolPolicy{MaxCallsPerMinute: 1}

	// An outer turn of conv-a calls echo once, filling its window.
	outerReg := tools.NewRegistry()
	registerEchoTool(t, outerReg, "echo")
	outerTS := NewTurnState()
	outerTS.ConversationID = "conv-a"
	outer := NewProviderStageWithTurnState(mock.NewToolProvider("p", "m", false, nil), outerReg, policy,
		&ProviderConfig{CallRate: window}, nil, nil, outerTS)
	results, err := outer.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "o1", Name: "echo", Args: json.RawMessage(`{}`)}}, roundRef{round: 1})
	require.NoError(t, err)
	require.Empty(t, toolResultsByID(results)["o1"].Error)

	// A step of the same conversation's composition calls echo in the same minute.
	reg := prompt.NewRegistryWithRepository(newMockRepo())
	require.NoError(t, reg.RegisterConfig("a", &prompt.Config{Spec: prompt.Spec{
		TaskType: "a", SystemTemplate: "You help.", AllowedTools: []string{"echo"},
	}}))
	stepReg := tools.NewRegistry()
	echo := &countingEcho{}
	stepReg.RegisterExecutor(echo)
	require.NoError(t, stepReg.Register(&tools.ToolDescriptor{
		Name: "echo", Description: "echo", Mode: echo.Name(), InputSchema: json.RawMessage(`{"type":"object"}`),
	}))
	parent := NewTurnState()
	parent.ConversationID = "conv-a"
	prov := &loopingToolProvider{ToolProvider: mock.NewToolProvider("loop", "m", false, nil)}
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg, ToolRegistry: stepReg, ToolPolicy: policy, CallRate: window, ParentTurnState: parent,
		Provider: prov,
	})
	_, _ = exec(context.Background(), &composition.Step{
		ID: "s", Kind: composition.KindAgent, PromptTask: "a", Tools: []string{"echo"},
		Termination: &composition.Termination{ToolCalled: "echo"},
	}, json.RawMessage(`"go"`))
	require.Positive(t, prov.rounds.Load(), "the step's model called echo")
	assert.Zero(t, echo.runs.Load(), "the step's call is over the conversation's limit")
}
