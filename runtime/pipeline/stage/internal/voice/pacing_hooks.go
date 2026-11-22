package voice

import (
	"context"
	"time"
)

// funcClock is a pacingClock made of two functions (SetPacingClock).
type funcClock struct {
	nowFn   func() time.Time
	sleepFn func(ctx context.Context, d time.Duration) error
}

func (c funcClock) now() time.Time { return c.nowFn() }

func (c funcClock) sleep(ctx context.Context, d time.Duration) error { return c.sleepFn(ctx, d) }

// SetPacingClock makes s read the time from now, wait with sleep, and forward
// the first preroll chunks of each audio sequence without pacing. It lets
// stage's tests drive a pacer on a fake clock: they cannot reach its
// unexported fields from outside this package. stage does not re-export it.
func SetPacingClock(
	s *AudioPacingStage,
	now func() time.Time,
	sleep func(ctx context.Context, d time.Duration) error,
	preroll int,
) {
	s.clock = funcClock{nowFn: now, sleepFn: sleep}
	s.prerollChunks = preroll
}

// FlushesOnCut reports whether s drops the queued audio of a cut reply
// (WithFlushOnCut). stage does not re-export it.
func FlushesOnCut(s *AudioPacingStage) bool {
	return s.flushOnCut
}
