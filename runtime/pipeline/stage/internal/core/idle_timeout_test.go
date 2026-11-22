package core_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/stagetest"
)

func TestWithIdleTimeout_CancelsAfterIdle(t *testing.T) {
	const idle = 50 * time.Millisecond
	clock := &stagetest.FakeIdleTimer{}
	ctx, cancel, _ := core.WithIdleTimer(context.Background(), idle, clock.AfterFunc)
	defer cancel()

	clock.Advance(idle - time.Nanosecond)
	require.NoError(t, ctx.Err(), "the idle timer fired before its window ended")

	clock.Advance(time.Nanosecond)
	require.Error(t, ctx.Err(), "context should have been cancelled by idle timeout")
	assert.ErrorIs(t, context.Cause(ctx), core.ErrIdleTimeout)
}

func TestWithIdleTimeout_ResetPreventsCancel(t *testing.T) {
	const idle = 100 * time.Millisecond
	clock := &stagetest.FakeIdleTimer{}
	ctx, cancel, reset := core.WithIdleTimer(context.Background(), idle, clock.AfterFunc)
	defer cancel()

	// Activity every half-window for 5 windows' worth: the timer never fires.
	for i := 0; i < 10; i++ {
		clock.Advance(idle / 2)
		reset()
	}
	require.NoError(t, ctx.Err(), "context should not be cancelled while resets are active")

	// No activity for one more window: it fires.
	clock.Advance(idle)
	require.Error(t, ctx.Err(), "context should be cancelled once resets stop")
	assert.ErrorIs(t, context.Cause(ctx), core.ErrIdleTimeout)
}

func TestWithIdleTimeout_ExplicitCancelStopsTimer(t *testing.T) {
	ctx, cancel, _ := core.WithIdleTimeout(context.Background(), 5*time.Second)

	cancel()

	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context should be done after explicit cancel")
	}

	// Cause should NOT be ErrIdleTimeout — it was an explicit cancel
	assert.NotErrorIs(t, context.Cause(ctx), core.ErrIdleTimeout)
}

func TestWithIdleTimeout_ZeroTimeoutNeverFires(t *testing.T) {
	t.Parallel()
	ctx, cancel, reset := core.WithIdleTimeout(context.Background(), 0)
	defer cancel()

	// Reset should be a no-op, not panic
	reset()

	time.Sleep(50 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled when idle timeout is disabled")
	default:
		// Good — still alive
	}
}

func TestResetIdleFromContext_Present(t *testing.T) {
	var called atomic.Bool
	spy := func() { called.Store(true) }

	ctx := core.ContextWithIdleReset(context.Background(), spy, time.Second)
	core.ResetIdleFromContext(ctx)

	require.True(t, called.Load(), "reset func should have been called")
}

func TestResetIdleFromContext_Missing(t *testing.T) {
	// Should not panic when no reset func is in the context
	assert.NotPanics(t, func() {
		core.ResetIdleFromContext(context.Background())
	})
}

func TestKeepIdleAlive_ResetsUntilStopped(t *testing.T) {
	const timeout = 40 * time.Millisecond

	var resets atomic.Int64
	ctx := core.ContextWithIdleReset(context.Background(), func() { resets.Add(1) }, timeout)
	ticker := stagetest.NewFakeTicker()

	stop := core.KeepIdleAliveWith(ctx, ticker.Start)
	assert.Equal(t, timeout/2, ticker.Interval, "the heartbeat ticks every half-window")
	for range 3 {
		select {
		case ticker.Ticks <- time.Time{}: // the heartbeat takes each tick
		case <-time.After(idleProbeBound):
			t.Fatal("the heartbeat stopped taking ticks while running")
		}
	}
	stop()

	select {
	case <-ticker.Stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the heartbeat kept running after stop")
	}
	assert.Equal(t, int64(3), resets.Load(), "each tick while running resets the idle timer once")
	select {
	case ticker.Ticks <- time.Time{}:
		t.Fatal("the heartbeat took a tick after stop")
	default:
	}
}

func TestKeepIdleAlive_StopIsIdempotent(t *testing.T) {
	ctx := core.ContextWithIdleReset(context.Background(), func() {}, 20*time.Millisecond)

	stop := core.KeepIdleAlive(ctx)
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
	assert.NotPanics(t, func() { core.KeepIdleAliveWith(context.Background(), noTicker)() })

	// Idle control present but the timeout is disabled.
	var resets atomic.Int64
	ctx := core.ContextWithIdleReset(context.Background(), func() { resets.Add(1) }, 0)
	core.KeepIdleAliveWith(ctx, noTicker)()
	assert.Zero(t, resets.Load(), "no heartbeat should run when the idle timeout is disabled")
}

// idleProbeBound bounds how long a test waits for the heartbeat to act.
const idleProbeBound = 2 * time.Second

func TestKeepIdleAliveUpTo_StopsHoldingAfterMaxHold(t *testing.T) {
	const timeout = 40 * time.Millisecond // heartbeat every 20ms
	var resets atomic.Int64
	ctx := core.ContextWithIdleReset(context.Background(), func() { resets.Add(1) }, timeout)
	ticker := stagetest.NewFakeTicker()

	stop := core.KeepIdleAliveUpTo(ctx, 3*timeout/2, ticker.Start) // three heartbeats
	defer stop()
	for range 3 {
		select {
		case ticker.Ticks <- time.Time{}:
		case <-time.After(idleProbeBound):
			t.Fatal("the heartbeat stopped taking ticks before its hold ran out")
		}
	}
	select {
	case <-ticker.Stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the heartbeat kept holding past maxHold")
	}
	assert.Equal(t, int64(3), resets.Load())
}

func TestKeepIdleAliveUpTo_HoldShorterThanAHeartbeatStillHoldsOnce(t *testing.T) {
	var resets atomic.Int64
	ctx := core.ContextWithIdleReset(context.Background(), func() { resets.Add(1) }, time.Second)
	ticker := stagetest.NewFakeTicker()

	stop := core.KeepIdleAliveUpTo(ctx, time.Millisecond, ticker.Start)
	defer stop()
	ticker.Ticks <- time.Time{}
	select {
	case <-ticker.Stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the heartbeat kept holding past one beat")
	}
	assert.Equal(t, int64(1), resets.Load())
}

func TestKeepIdleAlive_EndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(core.ContextWithIdleReset(context.Background(), func() {}, time.Second))
	ticker := stagetest.NewFakeTicker()
	stop := core.KeepIdleAliveWith(ctx, ticker.Start)
	defer stop()

	cancel()
	select {
	case <-ticker.Stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the heartbeat outlived its context")
	}
}
