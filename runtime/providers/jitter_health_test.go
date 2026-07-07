package providers

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeJitter is a hand-driven jitterHealthCounters for reporter tests.
type fakeJitter struct{ u, us, d int64 }

func (f *fakeJitter) Underruns() int64       { return f.u }
func (f *fakeJitter) UnderrunSamples() int64 { return f.us }
func (f *fakeJitter) Drops() int64           { return f.d }

func TestJitterHealthReporter_EmitsDeltasOnce(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := NewStreamMetrics(reg, "test", nil)

	jb := &fakeJitter{}
	var r JitterHealthReporter // zero value ready

	// First tick: 2 underruns / 960 samples / 100 drops.
	jb.u, jb.us, jb.d = 2, 960, 100
	r.Report(m, jb, "output")

	if got := testutil.ToFloat64(m.FrameUnderrunsVec().WithLabelValues("output")); got != 2 {
		t.Errorf("underruns = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.FrameUnderrunSamplesVec().WithLabelValues("output")); got != 960 {
		t.Errorf("underrun samples = %v, want 960", got)
	}
	if got := testutil.ToFloat64(m.FrameDropsVec().WithLabelValues("output", "overflow")); got != 100 {
		t.Errorf("drops = %v, want 100", got)
	}

	// Second tick with unchanged cumulative counters emits NOTHING (no double count).
	r.Report(m, jb, "output")
	if got := testutil.ToFloat64(m.FrameUnderrunsVec().WithLabelValues("output")); got != 2 {
		t.Errorf("underruns after no-change tick = %v, want 2", got)
	}

	// Third tick advances cumulative counters; only the delta is emitted.
	jb.u, jb.us, jb.d = 5, 2400, 128
	r.Report(m, jb, "output")
	if got := testutil.ToFloat64(m.FrameUnderrunsVec().WithLabelValues("output")); got != 5 {
		t.Errorf("underruns after delta tick = %v, want 5", got)
	}
	if got := testutil.ToFloat64(m.FrameUnderrunSamplesVec().WithLabelValues("output")); got != 2400 {
		t.Errorf("underrun samples after delta tick = %v, want 2400", got)
	}
	if got := testutil.ToFloat64(m.FrameDropsVec().WithLabelValues("output", "overflow")); got != 128 {
		t.Errorf("drops after delta tick = %v, want 128", got)
	}
}

func TestJitterHealthReporter_NilMetricsSafe(t *testing.T) {
	t.Parallel()
	var r JitterHealthReporter
	// Nil StreamMetrics must be a no-op (methods are nil-safe), no panic.
	r.Report(nil, &fakeJitter{u: 1, us: 480, d: 1}, "output")
}
