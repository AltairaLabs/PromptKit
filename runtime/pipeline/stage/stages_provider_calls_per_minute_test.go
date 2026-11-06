package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

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

// kitCalls returns n calls to read_kit, with ids prefix0..prefix(n-1).
func kitCalls(prefix string, n int) []types.MessageToolCall {
	calls := make([]types.MessageToolCall, n)
	for i := range calls {
		calls[i] = kitCall(fmt.Sprintf("%s%d", prefix, i), "read_kit")
	}
	return calls
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

// admitted is how many calls to tool w counts for conversationID.
func admitted(w *CallRateWindow, conversationID, tool string) int {
	w.counts.mu.Lock()
	defer w.counts.mu.Unlock()
	return len(w.counts.admitted[callRateKey{conversationID: conversationID, tool: tool}])
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
	assert.Equal(t, readKitLimitReached, res["b1"].Error)
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

// A round's calls run in parallel, and each is admitted atomically: with a
// limit of 2, exactly two of twenty run.
func TestProviderStage_MaxCallsPerMinuteAdmitsParallelCallsAtomically(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit")
	stage.callRate.counts.now = newFakeClock().now
	stage.turnState.ConversationID = "conv-a"

	results, err := stage.executeToolCalls(context.Background(), kitCalls("p", 20), roundRef{round: 1})
	require.NoError(t, err)
	assert.Equal(t, 18, countLimitRefusals(results))
	assert.Equal(t, 2, admitted(stage.callRate, "conv-a", "read_kit"))
}

// A call held for approval is not counted by the stage: it is admitted when
// the host approves it. With a limit of 2, all twenty approval-gated calls
// reach the gate.
func TestProviderStage_MaxCallsPerMinuteDoesNotCountHeldCalls(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit")
	stage.callRate.counts.now = newFakeClock().now
	stage.turnState.ConversationID = "conv-a"
	checked := &atomic.Int32{}
	stage.config.ApprovalChecker = func(_ context.Context, _, name string, _ map[string]any) *tools.PendingToolInfo {
		checked.Add(1)
		return &tools.PendingToolInfo{Reason: "requires_approval", ToolName: name}
	}

	results, err := stage.executeToolCalls(context.Background(), kitCalls("h", 20), roundRef{round: 1})
	var pending *tools.ErrToolsPending
	require.ErrorAs(t, err, &pending)
	assert.Len(t, pending.Pending, 20, "every call is held")
	assert.Zero(t, countLimitRefusals(results))
	assert.EqualValues(t, 20, checked.Load())
	assert.Zero(t, admitted(stage.callRate, "conv-a", "read_kit"), "a held call counts only once approved")
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

// A call its executor defers is not counted by the stage: it is admitted when
// the host uses its result.
func TestProviderStage_MaxCallsPerMinuteDoesNotCountDeferredCalls(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil)
	stage.callRate.counts.now = newFakeClock().now
	stage.turnState.ConversationID = "conv-a"
	stage.toolRegistry.RegisterExecutor(deferringExecutor{})
	require.NoError(t, stage.toolRegistry.Register(&tools.ToolDescriptor{
		Name: "read_kit", Description: "test", InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "deferring",
	}))

	// One call a round: a deferring call counts while it is in flight, so
	// calls of one round could see each other there.
	for i := range 3 {
		results, err := stage.executeToolCalls(context.Background(), kitCalls(fmt.Sprintf("d%d-", i), 1),
			roundRef{round: 1})
		var pending *tools.ErrToolsPending
		require.ErrorAs(t, err, &pending, "call %d is deferred, not refused", i)
		assert.Zero(t, countLimitRefusals(results))
	}
	assert.Zero(t, admitted(stage.callRate, "conv-a", "read_kit"))
}

// A call that never reaches its executor (an unknown tool, invalid
// arguments) is not counted, so it cannot take the slot of a valid call in the
// same round.
func TestProviderStage_MaxCallsPerMinuteDoesNotCountCallsThatNeverRun(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 1}, nil)
	stage.callRate.counts.now = newFakeClock().now
	stage.turnState.ConversationID = "conv-a"
	require.NoError(t, stage.toolRegistry.Register(&tools.ToolDescriptor{
		Name: "read_kit", Description: "test", Mode: "mock",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}))

	calls := []types.MessageToolCall{kitCall("ok", "read_kit")}
	for i := range 8 {
		calls = append(calls,
			kitCall(fmt.Sprintf("u%d", i), "nope"),
			types.MessageToolCall{ID: fmt.Sprintf("v%d", i), Name: "read_kit", Args: json.RawMessage(`{}`)})
	}
	results, err := stage.executeToolCalls(context.Background(), calls, roundRef{round: 1})
	require.NoError(t, err)
	byID := toolResultsByID(results)
	assert.Empty(t, byID["ok"].Error, "the valid call runs")
	for i := range 8 {
		assert.Contains(t, byID[fmt.Sprintf("u%d", i)].Error, "tool not found")
		assert.NotContains(t, byID[fmt.Sprintf("v%d", i)].Error, "calls per minute")
	}
	assert.Zero(t, admitted(stage.callRate, "conv-a", "nope"))
	assert.Equal(t, 1, admitted(stage.callRate, "conv-a", "read_kit"), "only the valid call counts")
}

// A call the registry's own rate limit (tools.WithRateLimit) refuses never
// runs, so it is not counted.
func TestProviderStage_MaxCallsPerMinuteDoesNotCountRegistryLimitRefusals(t *testing.T) {
	registry := tools.NewRegistry(tools.WithRateLimit(1))
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "read_kit", Description: "test", InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "mock",
	}))
	ts := NewTurnState()
	ts.ConversationID = "conv-a"
	ts.AllowedTools = []string{"read_kit"}
	stage := NewProviderStageWithTurnState(mock.NewToolProvider("test", "model", false, nil), registry,
		&pipeline.ToolPolicy{MaxCallsPerMinute: 5}, &ProviderConfig{}, nil, nil, ts)
	stage.callRate.counts.now = newFakeClock().now

	results, err := stage.executeToolCalls(context.Background(), kitCalls("r", 3), roundRef{round: 1})
	require.NoError(t, err)
	refusedByRegistry := 0
	for _, r := range toolResultsByID(results) {
		if r.Error != "" {
			assert.Contains(t, r.Error, tools.ErrRateLimitExceeded.Error())
			refusedByRegistry++
		}
	}
	require.Equal(t, 2, refusedByRegistry, "the registry runs one call a minute")
	assert.Equal(t, 1, admitted(stage.callRate, "conv-a", "read_kit"), "only the call that ran counts")
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

func TestCallRateWindow_DropsIdleConversations(t *testing.T) {
	clock := newFakeClock()
	w := NewCallRateWindow()
	w.counts.now = clock.now
	require.NoError(t, w.Admit("conv-a", "read_kit", 1))
	require.Error(t, w.Admit("conv-a", "read_kit", 1))

	clock.t = clock.t.Add(2 * time.Minute)
	require.NoError(t, w.Admit("conv-b", "read_kit", 1))
	assert.Len(t, w.counts.admitted, 1, "an expired window is dropped, not kept forever")
}

// A window shared through ProviderConfig.CallRate is the one the stage counts
// in, and a call admitted there from outside the stage (a held call a human
// approved) uses up the limit like a call the stage ran.
func TestProviderStage_MaxCallsPerMinuteCountsAdmittedCalls(t *testing.T) {
	window := NewCallRateWindow()
	window.counts.now = newFakeClock().now
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 2}, nil, "read_kit", "list_kits")
	stage.config.CallRate = window
	stage.callRate = window // as NewProviderStageWithTurnState wires it from the config

	require.NoError(t, window.Admit("conv-a", "read_kit", 2))
	res := runCallsTurn(t, stage, "conv-a", kitCall("a1", "read_kit"), kitCall("a2", "read_kit"))
	errs := []string{res["a1"].Error, res["a2"].Error}
	assert.ElementsMatch(t, []string{"", readKitLimitReached}, errs, "the admitted call takes one of the two slots")
}

func TestCallRateWindow_Admit(t *testing.T) {
	var nilWindow *CallRateWindow
	assert.NoError(t, nilWindow.Admit("conv-a", "read_kit", 1), "a nil window admits every call")
	assert.Nil(t, nilWindow.Scoped("wf"))

	w := NewCallRateWindow()
	w.counts.now = newFakeClock().now
	for range 3 {
		require.NoError(t, w.Admit("conv-a", "read_kit", 0), "a limit of 0 is no limit")
	}
	assert.Empty(t, w.counts.admitted, "an unlimited call is not counted")

	require.NoError(t, w.Admit("conv-a", "read_kit", 2))
	require.NoError(t, w.Admit("conv-a", "read_kit", 2))
	err := w.Admit("conv-a", "read_kit", 2)
	var limitErr *CallRateLimitError
	require.ErrorAs(t, err, &limitErr)
	assert.Equal(t, CallRateLimitError{Tool: "read_kit", Limit: 2}, *limitErr)
	assert.Equal(t, readKitLimitReached, err.Error())
	assert.Equal(t, 2, admitted(w, "conv-a", "read_kit"), "a refused call is not counted")
}

// An admission counts for one minute from when it was made, so the window
// frees one slot at a time.
func TestCallRateWindow_Slides(t *testing.T) {
	clock := newFakeClock()
	w := NewCallRateWindow()
	w.counts.now = clock.now
	require.NoError(t, w.Admit("conv-a", "read_kit", 2))
	clock.t = clock.t.Add(30 * time.Second)
	require.NoError(t, w.Admit("conv-a", "read_kit", 2))
	require.Error(t, w.Admit("conv-a", "read_kit", 2))

	clock.t = clock.t.Add(31 * time.Second)
	assert.NoError(t, w.Admit("conv-a", "read_kit", 2), "the first admission aged out")
	assert.Error(t, w.Admit("conv-a", "read_kit", 2), "the second has not")
}

// A scoped view counts every conversation's calls under its scope, in the
// counts of the window it views.
func TestCallRateWindow_ScopedSharesOneLimit(t *testing.T) {
	window := NewCallRateWindow()
	window.counts.now = newFakeClock().now
	scoped := window.Scoped("wf")

	require.NoError(t, scoped.Admit("conv-a", "read_kit", 2))
	require.NoError(t, scoped.Admit("conv-b", "read_kit", 2))
	assert.Error(t, scoped.Admit("conv-c", "read_kit", 2), "the three conversations share the scope's limit")
	assert.NoError(t, window.Admit("conv-a", "read_kit", 1), "the unscoped window keys on the conversation as before")
}

func TestNewProviderStage_UsesConfiguredCallRate(t *testing.T) {
	window := NewCallRateWindow()
	stage := NewProviderStageWithTurnState(nil, nil, nil, &ProviderConfig{CallRate: window}, nil, nil, nil)
	assert.Same(t, window, stage.callRate)
	assert.NotNil(t, NewProviderStageWithTurnState(nil, nil, nil, nil, nil, nil, nil).callRate,
		"without one the stage has its own")
}
