package provider

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// slowExecutor takes its time, like a tool that shells out to a build or waits
// on a third-party API.
type slowExecutor struct {
	name  string
	delay time.Duration
}

func (e *slowExecutor) Name() string { return e.name }

func (e *slowExecutor) Execute(
	ctx context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	select {
	case <-time.After(e.delay):
		return json.RawMessage(`{"result":"done"}`), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// windowExecutor is a tool that outlasts an idle window. It opens a window on
// the probe when it starts and closes it before it returns, recording whether
// the window held: whether something kept the idle timer alive while it ran.
type windowExecutor struct {
	probe *idleProbe
	held  atomic.Bool
}

func (e *windowExecutor) Name() string { return "window-executor" }

func (e *windowExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	e.probe.openWindow()
	e.held.Store(e.probe.closeWindow())
	return json.RawMessage(`{"result":"done"}`), nil
}

// stageWithSlowTool builds a provider stage whose only tool, slow_tool, runs exec.
func stageWithSlowTool(t *testing.T, exec tools.Executor) *ProviderStage {
	t.Helper()

	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "slow_tool",
		Description: "A tool that takes its time",
		Mode:        exec.Name(),
		InputSchema: []byte(`{"type": "object"}`),
		TimeoutMs:   60000,
	}))

	return NewProviderStage(mock.NewProvider("test", "model", false), registry, nil, nil)
}

// TestExecuteToolCalls_SlowToolDoesNotTripIdleTimeout is the regression for
// #2017: a tool that runs longer than IdleTimeout used to cancel its own turn,
// because nothing reset the idle timer while it worked. The tool holds an idle
// window open (see idleProbe) instead of racing a real timer. Reverting the
// `defer KeepIdleAlive(ctx)()` line in executeToolCalls makes it fail.
func TestExecuteToolCalls_SlowToolDoesNotTripIdleTimeout(t *testing.T) {
	probe := newIdleProbe()
	exec := &windowExecutor{probe: probe}
	stage := stageWithSlowTool(t, exec)

	results, err := stage.executeToolCalls(probe.context(context.Background()), []types.MessageToolCall{
		{ID: "call-1", Name: "slow_tool", Args: json.RawMessage(`{}`)},
	}, RoundRef{Round: 1})

	require.NoError(t, err, "a tool in flight is not an idle pipeline")
	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToolResult.Error)
	assert.True(t, exec.held.Load(), "the idle window lapsed while a tool was running")
}

// TestExecuteToolCalls_HeartbeatStopsWhenToolsFinish pins the other half: the
// heartbeat holds the timer open only while tools run. Once they are done the
// pipeline is idle again and the timer must fire as normal.
func TestExecuteToolCalls_HeartbeatStopsWhenToolsFinish(t *testing.T) {
	t.Parallel()
	const idle = 60 * time.Millisecond

	idleCtx, cancel, reset := core.WithIdleTimeout(context.Background(), idle)
	defer cancel()
	ctx := core.ContextWithIdleReset(idleCtx, reset, idle)

	stage := stageWithSlowTool(t, &slowExecutor{name: "slow-executor", delay: 10 * time.Millisecond})

	_, err := stage.executeToolCalls(ctx, []types.MessageToolCall{
		{ID: "call-1", Name: "slow_tool", Args: json.RawMessage(`{}`)},
	}, RoundRef{Round: 1})
	require.NoError(t, err)

	select {
	case <-idleCtx.Done():
		assert.ErrorIs(t, context.Cause(idleCtx), core.ErrIdleTimeout)
	case <-time.After(2 * time.Second):
		t.Fatal("idle timer never fired after tool execution finished — heartbeat leaked")
	}
}
