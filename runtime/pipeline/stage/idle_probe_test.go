package stage

import (
	"context"
	"time"
)

// idleProbe stands in for the pipeline's idle timer through the seam stages
// already use, the idle control contextWithIdleReset puts on the context.
//
// The real timer races wall-clock time: a test that runs slow work against a
// 50ms window fails whenever the runner is loaded enough to delay a heartbeat.
// With the probe the test decides when an idle window opens and closes. The
// window held if a reset arrived while it was open, and lapsed (the timer
// would have fired) if none did.
type idleProbe struct {
	resets chan struct{}
}

// idleProbeBound is how long closeWindow waits for a reset before it calls the
// window lapsed. It only limits how long a failing test takes: a passing one
// returns at the first reset.
const idleProbeBound = 2 * time.Second

// idleProbeTimeout is the idle window the probe reports. keepIdleAlive paces
// its heartbeat at half of it, so a small one keeps passing tests fast.
const idleProbeTimeout = 2 * time.Millisecond

func newIdleProbe() *idleProbe {
	return &idleProbe{resets: make(chan struct{}, 1)}
}

// context returns ctx carrying the probe as its idle control.
func (p *idleProbe) context(ctx context.Context) context.Context {
	return contextWithIdleReset(ctx, func() {
		select {
		case p.resets <- struct{}{}:
		default:
		}
	}, idleProbeTimeout)
}

// openWindow starts an idle window, forgetting resets that came before it.
func (p *idleProbe) openWindow() {
	for {
		select {
		case <-p.resets:
		default:
			return
		}
	}
}

// closeWindow ends the window and reports whether it held: a reset arrived
// after openWindow.
func (p *idleProbe) closeWindow() bool {
	select {
	case <-p.resets:
		return true
	case <-time.After(idleProbeBound):
		return false
	}
}
