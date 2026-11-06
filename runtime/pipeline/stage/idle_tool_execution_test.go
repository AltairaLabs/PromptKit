package stage

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
// `defer keepIdleAlive(ctx)()` line in executeToolCalls makes it fail.
func TestExecuteToolCalls_SlowToolDoesNotTripIdleTimeout(t *testing.T) {
	probe := newIdleProbe()
	exec := &windowExecutor{probe: probe}
	stage := stageWithSlowTool(t, exec)

	results, err := stage.executeToolCalls(probe.context(context.Background()), []types.MessageToolCall{
		{ID: "call-1", Name: "slow_tool", Args: json.RawMessage(`{}`)},
	}, roundRef{round: 1})

	require.NoError(t, err, "a tool in flight is not an idle pipeline")
	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToolResult.Error)
	assert.True(t, exec.held.Load(), "the idle window lapsed while a tool was running")
}

// TestExecuteToolCalls_HeartbeatStopsWhenToolsFinish pins the other half: the
// heartbeat holds the timer open only while tools run. Once they are done the
// pipeline is idle again and the timer must fire as normal.
func TestExecuteToolCalls_HeartbeatStopsWhenToolsFinish(t *testing.T) {
	const idle = 60 * time.Millisecond

	idleCtx, cancel, reset := withIdleTimeout(context.Background(), idle)
	defer cancel()
	ctx := contextWithIdleReset(idleCtx, reset, idle)

	stage := stageWithSlowTool(t, &slowExecutor{name: "slow-executor", delay: 10 * time.Millisecond})

	_, err := stage.executeToolCalls(ctx, []types.MessageToolCall{
		{ID: "call-1", Name: "slow_tool", Args: json.RawMessage(`{}`)},
	}, roundRef{round: 1})
	require.NoError(t, err)

	select {
	case <-idleCtx.Done():
		assert.ErrorIs(t, context.Cause(idleCtx), ErrIdleTimeout)
	case <-time.After(2 * time.Second):
		t.Fatal("idle timer never fired after tool execution finished — heartbeat leaked")
	}
}

// fakeTicker is a heartbeat ticker for keepIdleAliveWith that ticks only when
// the test sends on ticks. stopped closes when the heartbeat stops its ticker,
// which it does as it exits.
type fakeTicker struct {
	interval time.Duration
	ticks    chan time.Time
	stopped  chan struct{}
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ticks: make(chan time.Time), stopped: make(chan struct{})}
}

func (f *fakeTicker) start(d time.Duration) (<-chan time.Time, func()) {
	f.interval = d
	return f.ticks, func() { close(f.stopped) }
}

func TestKeepIdleAlive_ResetsUntilStopped(t *testing.T) {
	const timeout = 40 * time.Millisecond

	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) }, timeout)
	ticker := newFakeTicker()

	stop := keepIdleAliveWith(ctx, ticker.start)
	assert.Equal(t, timeout/2, ticker.interval, "the heartbeat ticks every half-window")
	for range 3 {
		select {
		case ticker.ticks <- time.Time{}: // the heartbeat takes each tick
		case <-time.After(idleProbeBound):
			t.Fatal("the heartbeat stopped taking ticks while running")
		}
	}
	stop()

	select {
	case <-ticker.stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the heartbeat kept running after stop")
	}
	assert.Equal(t, int64(3), resets.Load(), "each tick while running resets the idle timer once")
	select {
	case ticker.ticks <- time.Time{}:
		t.Fatal("the heartbeat took a tick after stop")
	default:
	}
}

func TestKeepIdleAlive_StopIsIdempotent(t *testing.T) {
	ctx := contextWithIdleReset(context.Background(), func() {}, 20*time.Millisecond)

	stop := keepIdleAlive(ctx)
	assert.NotPanics(t, func() {
		stop()
		stop()
	})
}

func TestKeepIdleAlive_NoIdleTimeoutConfigured(t *testing.T) {
	noTicker := func(time.Duration) (<-chan time.Time, func()) {
		t.Error("no heartbeat should run when the idle timeout is disabled")
		return nil, func() {}
	}

	// No idle control on the context at all.
	assert.NotPanics(t, func() { keepIdleAliveWith(context.Background(), noTicker)() })

	// Idle control present but the timeout is disabled.
	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) }, 0)
	keepIdleAliveWith(ctx, noTicker)()
	assert.Zero(t, resets.Load(), "no heartbeat should run when the idle timeout is disabled")
}
