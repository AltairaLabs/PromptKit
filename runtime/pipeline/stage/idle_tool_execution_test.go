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

// stageWithSlowTool builds a provider stage whose only tool sleeps for delay.
func stageWithSlowTool(t *testing.T, delay time.Duration) *ProviderStage {
	t.Helper()

	registry := tools.NewRegistry()
	registry.RegisterExecutor(&slowExecutor{name: "slow-executor", delay: delay})
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "slow_tool",
		Description: "A tool that takes its time",
		Mode:        "slow-executor",
		InputSchema: []byte(`{"type": "object"}`),
		TimeoutMs:   60000,
	}))

	return NewProviderStage(mock.NewProvider("test", "model", false), registry, nil, nil)
}

// TestExecuteToolCalls_SlowToolDoesNotTripIdleTimeout is the regression for
// #2017: a tool that runs longer than IdleTimeout used to cancel its own turn,
// because nothing reset the idle timer while it worked.
func TestExecuteToolCalls_SlowToolDoesNotTripIdleTimeout(t *testing.T) {
	const (
		idle      = 60 * time.Millisecond
		toolDelay = 300 * time.Millisecond // 5x the idle window
	)

	idleCtx, cancel, reset := withIdleTimeout(context.Background(), idle)
	defer cancel()
	ctx := contextWithIdleReset(idleCtx, reset, idle)

	stage := stageWithSlowTool(t, toolDelay)

	results, err := stage.executeToolCalls(ctx, []types.MessageToolCall{
		{ID: "call-1", Name: "slow_tool", Args: json.RawMessage(`{}`)},
	}, roundRef{round: 1})

	require.NoError(t, err, "a tool in flight is not an idle pipeline")
	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToolResult.Error)
	assert.NoError(t, idleCtx.Err(), "idle timer fired while a tool was running")
}

// TestExecuteToolCalls_HeartbeatStopsWhenToolsFinish pins the other half: the
// heartbeat holds the timer open only while tools run. Once they are done the
// pipeline is idle again and the timer must fire as normal.
func TestExecuteToolCalls_HeartbeatStopsWhenToolsFinish(t *testing.T) {
	const idle = 60 * time.Millisecond

	idleCtx, cancel, reset := withIdleTimeout(context.Background(), idle)
	defer cancel()
	ctx := contextWithIdleReset(idleCtx, reset, idle)

	stage := stageWithSlowTool(t, 10*time.Millisecond)

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

func TestKeepIdleAlive_ResetsUntilStopped(t *testing.T) {
	const interval = 20 * time.Millisecond

	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) }, interval)

	stop := keepIdleAlive(ctx)
	time.Sleep(6 * interval)
	stop()

	assert.Positive(t, resets.Load(), "heartbeat should have reset the idle timer while running")

	// A tick already in flight when stop() ran may still land, so take the
	// baseline after things settle rather than the instant stop() returns —
	// what matters is that resets cease, not their exact count.
	time.Sleep(4 * interval)
	settled := resets.Load()

	time.Sleep(6 * interval)
	assert.Equal(t, settled, resets.Load(), "heartbeat should stop resetting once stopped")
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
	// No idle control on the context at all.
	assert.NotPanics(t, func() { keepIdleAlive(context.Background())() })

	// Idle control present but the timeout is disabled.
	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) }, 0)
	stop := keepIdleAlive(ctx)
	time.Sleep(30 * time.Millisecond)
	stop()
	assert.Zero(t, resets.Load(), "no heartbeat should run when the idle timeout is disabled")
}
