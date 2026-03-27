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

func TestWithIdleTimeout_ResetPreventsCancel(t *testing.T) {
	ctx, cancel, reset := withIdleTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Reset every 50ms for 5 iterations (~250ms of activity).
	// The 100ms idle timeout should never fire during this period.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			time.Sleep(50 * time.Millisecond)
			reset()
		}
	}()

	// Context should still be alive while resets are happening
	<-done
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled while resets are active")
	default:
		// Good — still alive
	}

	// Now stop resetting. Context should cancel within ~150ms.
	select {
	case <-ctx.Done():
		assert.ErrorIs(t, context.Cause(ctx), ErrIdleTimeout)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("context should have been cancelled after resets stopped")
	}
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

	ctx := contextWithIdleReset(context.Background(), spy)
	ResetIdleFromContext(ctx)

	require.True(t, called.Load(), "reset func should have been called")
}

func TestResetIdleFromContext_Missing(t *testing.T) {
	// Should not panic when no reset func is in the context
	assert.NotPanics(t, func() {
		ResetIdleFromContext(context.Background())
	})
}
