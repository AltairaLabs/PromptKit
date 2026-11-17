package stage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// gatedDeferringExecutor defers every call without running it, as a client
// tool with no handler does (tools.DeferringExecutor). It holds the first call
// it is given until the second arrives, so the two are in flight together, or
// until a second has passed.
type gatedDeferringExecutor struct {
	entered atomic.Int32
	second  chan struct{}
	once    sync.Once
}

func (e *gatedDeferringExecutor) Name() string { return "gated-deferring" }

func (e *gatedDeferringExecutor) Defer(d *tools.ToolDescriptor, call tools.ToolCall) (
	*tools.ToolExecutionResult, bool,
) {
	res, _ := e.ExecuteAsync(context.Background(), d, call.Args)
	return res, true
}

func (e *gatedDeferringExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (
	json.RawMessage, error,
) {
	return nil, errors.New("gatedDeferringExecutor runs asynchronously only")
}

func (e *gatedDeferringExecutor) ExecuteAsync(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	if e.entered.Add(1) == 1 {
		select {
		case <-e.second:
		case <-time.After(time.Second):
		}
	} else {
		e.once.Do(func() { close(e.second) })
	}
	return &tools.ToolExecutionResult{
		Status:      tools.ToolStatusPending,
		PendingInfo: &tools.PendingToolInfo{Reason: "client_tool_deferred", ToolName: d.Name},
	}, nil
}

// Two deferred calls to one tool in one round, under a limit of one, both in
// their executor at once: neither is refused for the other, and neither
// counts until the host settles it.
func TestProviderStage_DeferredSiblingIsNotRefusedForAnInFlightCall(t *testing.T) {
	stage := stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxCallsPerMinute: 1}, nil)
	stage.offerState().setAllowed([]string{"read_kit"})
	stage.turnState.ConversationID = "conv-a"
	exec := &gatedDeferringExecutor{second: make(chan struct{})}
	stage.toolRegistry.RegisterExecutor(exec)
	require.NoError(t, stage.toolRegistry.Register(&tools.ToolDescriptor{
		Name: "read_kit", Description: "test", InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode: exec.Name(),
	}))

	results, err := stage.executeToolCalls(context.Background(), kitCalls("p", 2), roundRef{round: 1})
	var pending *tools.ErrToolsPending
	require.ErrorAs(t, err, &pending)
	assert.Len(t, pending.Pending, 2, "both calls are deferred")
	for _, r := range toolResultsByID(results) {
		assert.Empty(t, r.Error, "call %s is not refused", r.ID)
	}
	assert.Zero(t, admitted(stage.callRate, "conv-a", "read_kit"), "a deferred call counts only when settled")
}

// nestingExecutor runs, inside each read_kit call, a nested round that calls
// read_kit again on nested, a stage that shares the outer stage's window
// under the same scope, as a nested agent of the same conversation does.
type nestingExecutor struct {
	nested  *ProviderStage
	results []types.Message
}

func (e *nestingExecutor) Name() string { return "nesting" }

func (e *nestingExecutor) Execute(ctx context.Context, _ *tools.ToolDescriptor, _ json.RawMessage) (
	json.RawMessage, error,
) {
	if e.nested != nil {
		results, err := e.nested.executeToolCalls(ctx, kitCalls("n", 1), roundRef{round: 1})
		if err != nil {
			return nil, err
		}
		e.results = results
	}
	return json.RawMessage(`"ok"`), nil
}

// A nested call under the same window as the call it runs inside, over the
// limit, is refused at once: it never waits on its own parent.
func TestProviderStage_NestedCallUnderTheSameWindowIsRefusedNotBlocked(t *testing.T) {
	window := NewCallRateWindow()
	newStage := func(exec tools.Executor) *ProviderStage {
		registry := tools.NewRegistry()
		registry.RegisterExecutor(exec)
		require.NoError(t, registry.Register(&tools.ToolDescriptor{
			Name: "read_kit", Description: "test", InputSchema: json.RawMessage(`{"type":"object"}`),
			Mode: exec.Name(),
		}))
		ts := NewTurnState()
		ts.ConversationID = "conv-a"
		s := NewProviderStageWithTurnState(mock.NewToolProvider("test", "model", false, nil), registry,
			&pipeline.ToolPolicy{MaxCallsPerMinute: 1}, &ProviderConfig{CallRate: window.Scoped("conv")}, nil, nil, ts)
		s.offerState().setAllowed([]string{"read_kit"})
		return s
	}
	inner := &nestingExecutor{}
	outer := &nestingExecutor{nested: newStage(inner)}
	stage := newStage(outer)

	done := make(chan []types.Message, 1)
	go func() {
		results, err := stage.executeToolCalls(context.Background(), kitCalls("p", 1), roundRef{round: 1})
		assert.NoError(t, err)
		done <- results
	}()
	var results []types.Message
	select {
	case results = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the outer call waited on its own nested call")
	}
	assert.Empty(t, toolResultsByID(results)["p0"].Error, "the outer call runs")
	nested := toolResultsByID(outer.results)["n0"]
	require.NotNil(t, nested)
	assert.Equal(t, "Tool read_kit not executed: its limit of 1 calls per minute is reached", nested.Error,
		"the nested call over the limit is refused")
	assert.Equal(t, 1, admitted(window, "conv", "read_kit"))
}

// AdmitRelease admits as Admit does, and its release takes the admission
// back; with no limit it admits without counting.
func TestCallRateWindow_AdmitRelease(t *testing.T) {
	w := NewCallRateWindow()
	release, err := w.AdmitRelease("conv-a", "read_kit", 1)
	require.NoError(t, err)
	refused, err := w.AdmitRelease("conv-a", "read_kit", 1)
	var limitErr *CallRateLimitError
	require.ErrorAs(t, err, &limitErr)
	assert.Nil(t, refused)
	release()
	assert.Zero(t, admitted(w, "conv-a", "read_kit"), "the admission was taken back")

	free, err := w.AdmitRelease("conv-a", "read_kit", 0)
	require.NoError(t, err)
	free()
	assert.Zero(t, admitted(w, "conv-a", "read_kit"))
	var nilWindow *CallRateWindow
	_, err = nilWindow.AdmitRelease("conv-a", "read_kit", 1)
	assert.NoError(t, err)
}
