package stage

import (
	"context"
	"time"
)

// fakeClock simulates wall-clock time for the pacing and call-rate tests.
// now() is held in a field that callers can advance manually; sleep advances
// it by the requested duration unless onSleep overrides it. (A copy of
// internal/voice's: a stage test cannot use a voice test helper.)
type fakeClock struct {
	t time.Time
	// onSleep, if non-nil, is called for each sleep instead of advancing
	// time and returning nil.
	onSleep func(ctx context.Context, d time.Duration) error
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Unix(0, 0)}
}

func (f *fakeClock) now() time.Time { return f.t }

func (f *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if f.onSleep != nil {
		return f.onSleep(ctx, d)
	}
	f.t = f.t.Add(d)
	return nil
}
