package providers

// jitterHealthCounters is the read side of a realtime audio buffer's health
// counters, satisfied by *runtime/audio.JitterBuffer. It is declared here as an
// interface so this package need not import runtime/audio — keeping the audio
// package Prometheus-free (the metric wiring lives one layer up, here).
type jitterHealthCounters interface {
	Underruns() int64
	UnderrunSamples() int64
	Drops() int64
}

// JitterHealthReporter forwards the DELTAS of a jitter buffer's cumulative
// underrun/drop counters to StreamMetrics. A realtime audio consumer keeps one
// per buffer and calls Report once per pull/tick; it remembers the last-seen
// cumulative values so repeated calls never double-count.
//
// This is the reusable seam between any realtime audio consumer and the off-bus
// StreamMetrics layer — the PortAudio sample uses it, and so does the pipeline
// integration test. The zero value is ready to use. Not safe for concurrent
// use: call Report from the single consumer goroutine.
type JitterHealthReporter struct {
	prevUnderruns       int64
	prevUnderrunSamples int64
	prevDrops           int64
}

// Report emits the counter deltas since the last call to m (nil-safe) for the
// given direction ("input"/"output"). jb is the live jitter buffer. This is a
// DIRECT-UPDATE path: it never publishes to the event bus (see the off-bus
// invariant on StreamMetrics / AltairaLabs/PromptKit#853).
func (r *JitterHealthReporter) Report(m *StreamMetrics, jb jitterHealthCounters, direction string) {
	u := jb.Underruns()
	if d := u - r.prevUnderruns; d > 0 {
		for i := int64(0); i < d; i++ {
			m.FrameUnderrunInc(direction)
		}
		r.prevUnderruns = u
	}
	us := jb.UnderrunSamples()
	if d := us - r.prevUnderrunSamples; d > 0 {
		m.FrameUnderrunSamplesAdd(direction, int(d))
		r.prevUnderrunSamples = us
	}
	dr := jb.Drops()
	if d := dr - r.prevDrops; d > 0 {
		m.FrameDropAdd(direction, "overflow", int(d))
		r.prevDrops = dr
	}
}
