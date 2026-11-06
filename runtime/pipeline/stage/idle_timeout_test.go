package stage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithIdleTimeout_CancelsAfterIdle(t *testing.T) {
	ctx, cancel, _ := withIdleTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
		// Expected: idle timeout fired
	case <-time.After(300 * time.Millisecond):
		t.Fatal("context should have been cancelled by idle timeout")
	}

	assert.ErrorIs(t, context.Cause(ctx), ErrIdleTimeout)
}

// fakeIdleTimer is a virtual clock with one AfterFunc timer on it, for
// withIdleTimer. The timer fires only when advance moves the clock to its
// deadline, so a test decides exactly when the idle window ends relative to
// the resets it makes.
type fakeIdleTimer struct {
	now, deadline time.Duration
	armed         bool
	fire          func()
}

func (f *fakeIdleTimer) afterFunc(d time.Duration, fn func()) idleTimer {
	f.fire = fn
	f.Reset(d)
	return f
}

func (f *fakeIdleTimer) Stop() bool {
	was := f.armed
	f.armed = false
	return was
}

func (f *fakeIdleTimer) Reset(d time.Duration) bool {
	was := f.armed
	f.deadline, f.armed = f.now+d, true
	return was
}

// advance moves the clock on by d, firing the timer if its deadline falls in
// that time.
func (f *fakeIdleTimer) advance(d time.Duration) {
	f.now += d
	if f.armed && f.now >= f.deadline {
		f.armed = false
		f.fire()
	}
}

func TestWithIdleTimeout_ResetPreventsCancel(t *testing.T) {
	const idle = 100 * time.Millisecond
	clock := &fakeIdleTimer{}
	ctx, cancel, reset := withIdleTimer(context.Background(), idle, clock.afterFunc)
	defer cancel()

	// Activity every half-window for 5 windows' worth: the timer never fires.
	for i := 0; i < 10; i++ {
		clock.advance(idle / 2)
		reset()
	}
	require.NoError(t, ctx.Err(), "context should not be cancelled while resets are active")

	// No activity for one more window: it fires.
	clock.advance(idle)
	require.Error(t, ctx.Err(), "context should be cancelled once resets stop")
	assert.ErrorIs(t, context.Cause(ctx), ErrIdleTimeout)
}

func TestWithIdleTimeout_ExplicitCancelStopsTimer(t *testing.T) {
	ctx, cancel, _ := withIdleTimeout(context.Background(), 5*time.Second)

	cancel()

	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context should be done after explicit cancel")
	}

	// Cause should NOT be ErrIdleTimeout — it was an explicit cancel
	assert.NotErrorIs(t, context.Cause(ctx), ErrIdleTimeout)
}

func TestWithIdleTimeout_ZeroTimeoutNeverFires(t *testing.T) {
	ctx, cancel, reset := withIdleTimeout(context.Background(), 0)
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

	ctx := contextWithIdleReset(context.Background(), spy, time.Second)
	ResetIdleFromContext(ctx)

	require.True(t, called.Load(), "reset func should have been called")
}

func TestResetIdleFromContext_Missing(t *testing.T) {
	// Should not panic when no reset func is in the context
	assert.NotPanics(t, func() {
		ResetIdleFromContext(context.Background())
	})
}
