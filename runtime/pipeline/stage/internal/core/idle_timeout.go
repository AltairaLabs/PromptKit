package core

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrIdleTimeout is returned (via context.Cause) when the pipeline idle timeout
// fires because no activity was detected within the configured duration.
var ErrIdleTimeout = errors.New("pipeline idle timeout: no activity detected")

// IdleResetKey is the context key for the idle reset function.
type IdleResetKey struct{}

// WithIdleTimeout creates a context that is cancelled when no activity occurs
// within the given timeout. The returned reset function resets the timer —
// call it on each activity signal (stream chunk, round completion, etc.).
//
// If timeout <= 0, idle timeout is disabled: the returned context is a plain
// cancellable context and the reset function is a no-op.
func WithIdleTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc, func()) {
	return WithIdleTimer(parent, timeout, func(d time.Duration, f func()) IdleTimer { return time.AfterFunc(d, f) })
}

// IdleTimer is the part of *time.Timer the idle timeout uses.
type IdleTimer interface {
	Stop() bool
	Reset(d time.Duration) bool
}

// WithIdleTimer is WithIdleTimeout with the timer it starts supplied by
// afterFunc, which has time.AfterFunc's contract. Tests pass a fake clock so
// they decide when the timer fires instead of racing a real one.
func WithIdleTimer(
	parent context.Context, timeout time.Duration, afterFunc func(time.Duration, func()) IdleTimer,
) (context.Context, context.CancelFunc, func()) {
	if timeout <= 0 {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, func() { /* no-op reset: idle timeout disabled */ }
	}

	ctx, cancelCause := context.WithCancelCause(parent)

	timer := afterFunc(timeout, func() {
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

// IdleControl is what the idle timer puts on the context: the reset function
// stages call to signal activity, plus the window it resets to — KeepIdleAlive
// needs the window to pick a heartbeat interval.
type IdleControl struct {
	Reset   func()
	timeout time.Duration
}

// minIdleHeartbeatInterval floors the heartbeat so a pathologically small idle
// timeout cannot spin the ticker.
const minIdleHeartbeatInterval = time.Millisecond

// ContextWithIdleReset stores the idle control in the context so that
// downstream stages can call ResetIdleFromContext to signal activity.
func ContextWithIdleReset(ctx context.Context, reset func(), timeout time.Duration) context.Context {
	return context.WithValue(ctx, IdleResetKey{}, IdleControl{Reset: reset, timeout: timeout})
}

// ResetIdleFromContext extracts the idle reset function from the context and
// calls it. This is a no-op if no idle timeout is configured.
func ResetIdleFromContext(ctx context.Context) {
	if c, ok := ctx.Value(IdleResetKey{}).(IdleControl); ok && c.Reset != nil {
		c.Reset()
	}
}

// KeepIdleAlive holds the idle timer open across work that is active but
// silent, resetting it every half-window until the returned stop func runs.
// A tool call in flight is not an idle pipeline: the timer exists to catch a
// pipeline that has stalled, so a tool call slower than IdleTimeout does not
// cancel its own turn.
//
// A hung tool stays bounded. Every registered tool carries a TimeoutMs
// (tools.DefaultToolTimeout when it declares none), so a tool that never
// returns is cut off by its own deadline. ExecutionTimeout remains the ceiling
// on the whole turn.
//
// Returns a no-op stop when no idle timeout is configured.
func KeepIdleAlive(ctx context.Context) (stop func()) {
	return KeepIdleAliveWith(ctx, RealIdleTicker)
}

// KeepIdleAliveWith is KeepIdleAlive with its heartbeat ticker started by
// newTicker, which returns the tick channel and the function that stops it.
// Tests pass a fake ticker so they decide when the heartbeat ticks.
func KeepIdleAliveWith(
	ctx context.Context, newTicker func(time.Duration) (<-chan time.Time, func()),
) (stop func()) {
	return KeepIdleAliveUpTo(ctx, 0, newTicker)
}

// RealIdleTicker starts a real heartbeat ticker for KeepIdleAliveUpTo.
func RealIdleTicker(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}

// KeepIdleAliveUpTo is KeepIdleAliveWith with the hold bounded: the heartbeat
// stops resetting the idle timer once it has held it for maxHold, so the timer
// fires at most maxHold plus one idle window after the hold began. maxHold <= 0
// holds until stop, as KeepIdleAlive does.
func KeepIdleAliveUpTo(
	ctx context.Context, maxHold time.Duration, newTicker func(time.Duration) (<-chan time.Time, func()),
) (stop func()) {
	c, ok := ctx.Value(IdleResetKey{}).(IdleControl)
	if !ok || c.Reset == nil || c.timeout <= 0 {
		return func() { /* no-op: idle timeout disabled */ }
	}

	interval := max(c.timeout/2, minIdleHeartbeatInterval)
	maxTicks := 0 // unbounded
	if maxHold > 0 {
		maxTicks = max(int(maxHold/interval), 1)
	}

	done := make(chan struct{})
	ticks, stopTicker := newTicker(interval)
	go func() {
		defer stopTicker()
		for n := 0; ; {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticks:
				c.Reset()
				if n++; maxTicks > 0 && n >= maxTicks {
					return
				}
			}
		}
	}()

	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
