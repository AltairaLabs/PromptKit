package stage

import (
	"context"
	"time"
)

// canceledForwardTimeout bounds each send once the turn's context is done. A
// variable so tests need not wait it out.
var canceledForwardTimeout = 500 * time.Millisecond

// cancelForwarder forwards a pass-through stage's elements so that a
// canceled turn still delivers what it produced.
//
// When a turn is canceled (the caller gives up, the idle timer fires, a
// barge-in), the provider stage emits the reply the model had started and an
// error after the cancellation. Every stage downstream of it must carry those
// through to the save stage and the caller collecting the result: a stage
// that returned on ctx.Done would drop the partial reply and the error, and
// block the provider stage sending them.
//
// So after cancellation a stage keeps reading its input to the end, and
// offers each element downstream for a short time. A consumer collecting the
// result still reads; one that has gone away must not block the pipeline, so
// the first send that times out stops all further forwarding.
type cancelForwarder struct {
	gone bool
}

// forward sends elem downstream: a normal cancellable send while ctx is live,
// and a bounded one after it is done.
func (f *cancelForwarder) forward(ctx context.Context, output chan<- StreamElement, elem StreamElement) {
	if f.gone {
		return
	}
	if ctx.Err() == nil {
		select {
		case output <- elem:
			return
		case <-ctx.Done():
		}
	}
	f.gone = !forwardAfterCancel(output, elem)
}

// forwardAfterCancel offers elem downstream for at most
// canceledForwardTimeout and reports whether it was delivered.
func forwardAfterCancel(output chan<- StreamElement, elem StreamElement) bool {
	timer := time.NewTimer(canceledForwardTimeout)
	defer timer.Stop()
	select {
	case output <- elem:
		return true
	case <-timer.C:
		return false
	}
}

// liveContext returns ctx, or a copy the cancellation does not reach when ctx
// is done — for the side effects (recording, events) that describe a canceled
// turn and must not fail because it was canceled.
func liveContext(ctx context.Context) context.Context {
	if ctx.Err() != nil {
		return context.WithoutCancel(ctx)
	}
	return ctx
}
