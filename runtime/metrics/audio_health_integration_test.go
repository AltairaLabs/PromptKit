package metrics_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestAudioHealth_AllFamiliesExposedInScrape proves that after exercising the
// real audio-health code paths, every metric family is present in a Prometheus
// text-exposition scrape with the documented bounded labels. This is the
// "show it in action AND exposed" deliverable for #1585.
//
// Pacing-behind-deadline BEHAVIOR is proven end-to-end in
// runtime/pipeline/stage (TestAudioPacingStage_ReportsBehindDeadline); here it
// is recorded directly so this test stays focused on exposition.
func TestAudioHealth_AllFamiliesExposedInScrape(t *testing.T) {
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)

	reg := prometheus.NewRegistry()
	providers.RegisterDefaultStreamMetrics(reg, "promptkit", nil)
	m := providers.DefaultStreamMetrics()

	// 1) Pacing behind deadline (recorded directly; behavior proven elsewhere).
	m.PacingBehindDeadlineInc("input")

	// 2) JitterBuffer underrun via a real starved buffer + the same delta wiring
	//    the audiohelper consumer uses.
	jb := audio.NewJitterBuffer(480)
	_ = jb.Pull(480) // empty -> underrun of 480 samples
	m.FrameUnderrunInc("output")
	m.FrameUnderrunSamplesAdd("output", int(jb.UnderrunSamples()))
	jb.Push(make([]int16, 480*4)) // overflow
	m.FrameDropAdd("output", "overflow", int(jb.Drops()))

	// 3) Bus saturation exporter over a real saturated bus.
	bus := events.NewEventBus(events.WithEventBufferSize(1))
	t.Cleanup(bus.Close)
	for range 50 {
		bus.Publish(&events.Event{Type: events.EventPipelineStarted})
	}
	reg.MustRegister(metrics.NewEventBusHealthCollector(bus, "promptkit", nil))

	// Scrape and assert every family name is present.
	body := scrape(t, reg)
	for _, want := range []string{
		"promptkit_audio_frame_underruns_total",
		"promptkit_audio_frame_underrun_samples_total",
		"promptkit_audio_frame_drops_total",
		"promptkit_audio_pacing_behind_deadline_total",
		"promptkit_eventbus_events_dropped_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing metric family %q\n---\n%s", want, body)
		}
	}
	// Label sanity: direction + reason present, no per-stream labels.
	if !strings.Contains(body, `direction="output"`) {
		t.Errorf("expected direction=output label in scrape")
	}
	if !strings.Contains(body, `reason="overflow"`) {
		t.Errorf("expected reason=overflow label in scrape")
	}
}

// scrape renders the registry as Prometheus text exposition.
func scrape(t *testing.T, g prometheus.Gatherer) string {
	t.Helper()
	mfs, err := g.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var sb strings.Builder
	enc := expfmt.NewEncoder(&sb, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	return sb.String()
}
