package stage

import (
	"context"
	"errors"
	"time"
)

// ErrIdleTimeout is returned (via context.Cause) when the pipeline idle timeout
// fires because no activity was detected within the configured duration.
var ErrIdleTimeout = errors.New("pipeline idle timeout: no activity detected")

// idleResetKey is the context key for the idle reset function.
type idleResetKey struct{}

// withIdleTimeout creates a context that is cancelled when no activity occurs
// within the given timeout. The returned reset function resets the timer —
// call it on each activity signal (stream chunk, round completion, etc.).
//
// If timeout <= 0, idle timeout is disabled: the returned context is a plain
// cancellable context and the reset function is a no-op.
func withIdleTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc, func()) {
	if timeout <= 0 {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, func() {}
	}

	ctx, cancelCause := context.WithCancelCause(parent)

	timer := time.AfterFunc(timeout, func() {
		cancelCause(ErrIdleTimeout)
	})

	cancel := func() {
		timer.Stop()
		cancelCause(nil)
	}

	reset := func() {
		timer.Reset(timeout)
	}

	return ctx, cancel, reset
}

// contextWithIdleReset stores the idle reset function in the context so that
// downstream stages can call ResetIdleFromContext to signal activity.
func contextWithIdleReset(ctx context.Context, reset func()) context.Context {
	return context.WithValue(ctx, idleResetKey{}, reset)
}

// ResetIdleFromContext extracts the idle reset function from the context and
// calls it. This is a no-op if no idle timeout is configured.
func ResetIdleFromContext(ctx context.Context) {
	if fn, ok := ctx.Value(idleResetKey{}).(func()); ok {
		fn()
	}
}
