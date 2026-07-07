package stage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestAudioTelemetry_NeverPublishesToBus asserts that driving many audio
// frames through the instrumented pacing stage produces ZERO events on the
// bus. Per-frame audio telemetry is direct-update only (see #853). If a future
// change routes audio health through bus.Publish, this test fails.
func TestAudioTelemetry_NeverPublishesToBus(t *testing.T) {
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	providers.RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	bus := events.NewEventBus(events.WithEventBufferSize(1024))
	t.Cleanup(bus.Close)
	var published atomic.Int64 // atomic: SubscribeAll delivers on worker goroutines
	// events.Listener is `func(*Event)`, so a plain func value satisfies it.
	bus.SubscribeAll(func(_ *events.Event) { published.Add(1) })

	// Over-advancing clock so most chunks hit the behind-deadline branch.
	clock := newFakeClock()
	clock.onSleep = func(_ context.Context, d time.Duration) error {
		clock.t = clock.t.Add(d + 100*time.Millisecond)
		return nil
	}

	const n = 500
	in := make([]StreamElement, n)
	for i := range in {
		in[i] = audioElem(2400, 24000)
	}
	if _, _, err := runPacingWithPreroll(t, in, clock, 0); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	// Give any (erroneously published) events a chance to be delivered.
	time.Sleep(20 * time.Millisecond)
	if got := published.Load(); got != 0 {
		t.Errorf("audio path published %d events to the bus; want 0 (off-bus invariant)", got)
	}
}

// TestAudioTelemetry_PerFrameAllocBounded asserts the per-frame metric calls
// are allocation-bounded and independent of stream count — the property that
// keeps 2k concurrent streams from OOMing.
func TestAudioTelemetry_PerFrameAllocBounded(t *testing.T) {
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	providers.RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)
	m := providers.DefaultStreamMetrics()

	perFrame := func() {
		m.FrameUnderrunInc("output")
		m.FrameUnderrunSamplesAdd("output", 480)
		m.FrameDropAdd("output", "overflow", 1)
		m.PacingBehindDeadlineInc("output")
	}

	// WithLabelValues on a warmed vec allocates a bounded, small amount and does
	// not grow with call count. Warm the label sets first.
	perFrame()
	const maxAllocsPerFrame = 8 // documented ceiling; bounded, not zero
	if got := testing.AllocsPerRun(1000, perFrame); got > maxAllocsPerFrame {
		t.Errorf("per-frame audio metric allocs = %v, want <= %d (bounded)", got, maxAllocsPerFrame)
	}
}
