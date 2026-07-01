package portaudio

import "time"

// sampleClock converts a cumulative PCM sample count at a fixed rate to a
// monotonic presentation timestamp (PTS). It is intentionally unexported —
// only the duplex session loop uses it, always from a single goroutine.
type sampleClock struct {
	rate int
	n    int64
}

// newSampleClock returns a zeroed sampleClock ticking at the given sample rate.
func newSampleClock(rate int) *sampleClock {
	return &sampleClock{rate: rate}
}

// advance adds samples to the running count.
func (c *sampleClock) advance(samples int64) {
	c.n += samples
}

// pts returns the presentation timestamp for the current sample count.
// Uses integer arithmetic to avoid floating-point rounding: the expression
// n * (1s / rate) is equivalent to n * time.Second / rate.
func (c *sampleClock) pts() time.Duration {
	return time.Duration(c.n) * time.Second / time.Duration(c.rate)
}
