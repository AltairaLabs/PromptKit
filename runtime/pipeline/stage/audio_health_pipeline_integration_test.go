package stage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/expfmt"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

// TestAudioHealthPipeline_EndToEnd drives ONE real duplex flow and asserts that
// every audio-health metric family populates from production code — not from
// direct metric calls — and is present in a Prometheus scrape.
//
// Flow (all real components):
//
//	mock provider (emits PCM audio output chunks)
//	  -> DuplexProviderStage.Process        (renders chunks -> audio StreamElements)
//	  -> AudioPacingStage "audio-pacing-output" with an over-advancing clock
//	         => production paceFor emits audio_pacing_behind_deadline_total{output}
//	  -> a real audio.JitterBuffer drained by providers.JitterHealthReporter
//	         => production Report emits audio_frame_underruns/underrun_samples/drops
//	plus a real saturated EventBus + NewEventBusHealthCollector
//	         => eventbus_events_dropped_total
//
// The consumer's pull cadence is test-authored because there is no production
// realtime consumer loop in this repo (the serving path streams audio out over
// a transport); the underrun/drop *emission* is the shared production
// JitterHealthReporter, and the JitterBuffer is the production primitive.
func TestAudioHealthPipeline_EndToEnd(t *testing.T) {
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	reg := prometheus.NewRegistry()
	providers.RegisterDefaultStreamMetrics(reg, "promptkit", nil)
	m := providers.DefaultStreamMetrics()

	// --- Real duplex provider stage emitting 6 output audio chunks then finish ---
	const sampleRate = 24000
	audioChunk := func() providers.StreamChunk {
		return providers.StreamChunk{
			MediaData: &providers.StreamMediaData{
				Data:       make([]byte, 2400), // 1200 samples = 50 ms @24 kHz
				MIMEType:   "audio/pcm",
				SampleRate: sampleRate,
				Channels:   1,
			},
		}
	}
	finish := "stop"
	chunks := []providers.StreamChunk{
		audioChunk(), audioChunk(), audioChunk(),
		audioChunk(), audioChunk(), audioChunk(),
		{FinishReason: &finish},
	}
	inner := providersmock.NewStreamingProvider("dtest", "mock-model", false)
	inner.WithAutoRespond("ok")
	inner.WithCloseAfterTurns(1)
	provider := &chunkInjectingProvider{StreamingProvider: inner, chunks: chunks}
	dstage := NewDuplexProviderStage(provider, baseConfig())

	// --- Real pacing-OUTPUT stage; over-advancing clock => consumer behind ---
	pacing := NewNamedAudioPacingStage("audio-pacing-output")
	fc := newFakeClock()
	fc.onSleep = func(_ context.Context, d time.Duration) error {
		fc.t = fc.t.Add(d + 100*time.Millisecond)
		return nil
	}
	pacing.clock = fc
	pacing.prerollChunks = 0

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dinput := make(chan StreamElement, 4)
	dmid := make(chan StreamElement, 32)
	pout := make(chan StreamElement, 32)

	go func() { _ = dstage.Process(ctx, dinput, dmid) }()
	go func() { _ = pacing.Process(ctx, dmid, pout) }()

	// User speaks one turn, then end-of-input triggers the mock's audio response.
	dinput <- StreamElement{Audio: &AudioData{
		Samples: []byte("hello there!!!!"), SampleRate: 16000, Format: AudioFormatPCM16,
	}}
	dinput <- StreamElement{EndOfStream: true}
	close(dinput)

	// Collect the paced assistant audio chunks.
	var paced []StreamElement
	for e := range pout {
		if e.Audio != nil && len(e.Audio.Samples) > 0 {
			paced = append(paced, e)
		}
	}
	if len(paced) == 0 {
		t.Fatal("no paced audio came out of the duplex->pacing pipeline")
	}

	// --- Real jitter consumer: production JitterHealthReporter over a real buffer ---
	jb := audio.NewJitterBuffer(1200) // one chunk of capacity
	var rep providers.JitterHealthReporter
	for _, e := range paced {
		jb.Push(make([]int16, len(e.Audio.Samples)/2)) // sample count from the real chunk
		_ = jb.Pull(1200)
		rep.Report(m, jb, "output")
	}
	_ = jb.Pull(1200) // buffer now empty -> production reporter records an underrun
	rep.Report(m, jb, "output")
	jb.Push(make([]int16, 1200*4)) // burst past capacity -> production reporter records drops
	rep.Report(m, jb, "output")

	// --- Real saturated event bus + pull collector ---
	bus := events.NewEventBus(events.WithEventBufferSize(1))
	t.Cleanup(bus.Close)
	for range 50 {
		bus.Publish(&events.Event{Type: events.EventPipelineStarted})
	}
	reg.MustRegister(metrics.NewEventBusHealthCollector(bus, "promptkit", nil))

	// --- Assert each signal was recorded by PRODUCTION code ---
	if got := testutil.ToFloat64(m.PacingBehindDeadlineVec().WithLabelValues("output")); got < 1 {
		t.Errorf("audio_pacing_behind_deadline_total{output} = %v, want >= 1 (from real pacing stage)", got)
	}
	if got := testutil.ToFloat64(m.FrameUnderrunsVec().WithLabelValues("output")); got < 1 {
		t.Errorf("audio_frame_underruns_total{output} = %v, want >= 1 (from real reporter)", got)
	}
	if got := testutil.ToFloat64(m.FrameUnderrunSamplesVec().WithLabelValues("output")); got < 1200 {
		t.Errorf("audio_frame_underrun_samples_total{output} = %v, want >= 1200", got)
	}
	if got := testutil.ToFloat64(m.FrameDropsVec().WithLabelValues("output", "overflow")); got < 1 {
		t.Errorf("audio_frame_drops_total{output,overflow} = %v, want >= 1", got)
	}

	// --- Assert every family is present in a real Prometheus scrape ---
	body := scrapeText(t, reg)
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
	if !strings.Contains(body, `direction="output"`) {
		t.Errorf("expected direction=output label in scrape")
	}
	if !strings.Contains(body, `reason="overflow"`) {
		t.Errorf("expected reason=overflow label in scrape")
	}
	t.Logf("end-to-end scrape:\n%s", filterAudioLines(body))
}

// scrapeText renders a gatherer as Prometheus text exposition.
func scrapeText(t *testing.T, g prometheus.Gatherer) string {
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

// filterAudioLines keeps only the audio-health / eventbus metric sample lines.
func filterAudioLines(body string) string {
	var keep []string
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(ln, "promptkit_audio_") || strings.HasPrefix(ln, "promptkit_eventbus_") {
			keep = append(keep, ln)
		}
	}
	return strings.Join(keep, "\n")
}
