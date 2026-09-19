package stage

import (
	"context"
	"errors"
	"sync"
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
		return ctx, cancel, func() { /* no-op reset: idle timeout disabled */ }
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

// idleControl is what the idle timer puts on the context: the reset function
// stages call to signal activity, plus the window it resets to — keepIdleAlive
// needs the window to pick a heartbeat interval.
type idleControl struct {
	reset   func()
	timeout time.Duration
}

// minIdleHeartbeatInterval floors the heartbeat so a pathologically small idle
// timeout cannot spin the ticker.
const minIdleHeartbeatInterval = time.Millisecond

// contextWithIdleReset stores the idle control in the context so that
// downstream stages can call ResetIdleFromContext to signal activity.
func contextWithIdleReset(ctx context.Context, reset func(), timeout time.Duration) context.Context {
	return context.WithValue(ctx, idleResetKey{}, idleControl{reset: reset, timeout: timeout})
}

// ResetIdleFromContext extracts the idle reset function from the context and
// calls it. This is a no-op if no idle timeout is configured.
func ResetIdleFromContext(ctx context.Context) {
	if c, ok := ctx.Value(idleResetKey{}).(idleControl); ok && c.reset != nil {
		c.reset()
	}
}

// keepIdleAlive holds the idle timer open across work that is active but
// silent, resetting it every half-window until the returned stop func runs.
// A tool call in flight is not an idle pipeline: the timer exists to catch a
// pipeline that has stalled, and before this a single tool call slower than
// IdleTimeout cancelled its own turn (#2017).
//
// This does not leave a hung tool unbounded. Every registered tool carries a
// TimeoutMs (tools.DefaultToolTimeout when it declares none), so a tool that
// never returns is cut off by its own deadline — which is the bound that
// should catch it. ExecutionTimeout remains the ceiling on the whole turn.
//
// Returns a no-op stop when no idle timeout is configured.
func keepIdleAlive(ctx context.Context) (stop func()) {
	c, ok := ctx.Value(idleResetKey{}).(idleControl)
	if !ok || c.reset == nil || c.timeout <= 0 {
		return func() { /* no-op: idle timeout disabled */ }
	}

	interval := max(c.timeout/2, minIdleHeartbeatInterval)

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.reset()
			}
		}
	}()

	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
