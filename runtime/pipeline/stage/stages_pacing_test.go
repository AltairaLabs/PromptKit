package stage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// fakeClock simulates wall-clock time for AudioPacingStage tests. now()
// is held in a field that callers can advance manually; sleep advances
// it by the requested duration unless overridden, so the
// past-the-deadline branch is reachable.
type fakeClock struct {
	t      time.Time
	delays []time.Duration
	// onSleep, if non-nil, is called for each sleep instead of advancing
	// time and returning nil. Lets a test inject ctx cancellation or
	// custom time progression.
	onSleep func(ctx context.Context, d time.Duration) error
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Unix(0, 0)}
}

func (f *fakeClock) now() time.Time { return f.t }

func (f *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if d > 0 {
		f.delays = append(f.delays, d)
	}
	if f.onSleep != nil {
		return f.onSleep(ctx, d)
	}
	f.t = f.t.Add(d)
	return nil
}

// audioElem builds a PCM16 mono audio StreamElement carrying byteCount
// bytes at the given sample rate.
func audioElem(byteCount, sampleRate int) StreamElement {
	return StreamElement{
		Audio: &AudioData{
			Samples:    make([]byte, byteCount),
			SampleRate: sampleRate,
			Channels:   1,
			Format:     AudioFormatPCM16,
		},
	}
}

// runPacing executes the stage against a fixed list of input elements
// using a fakeClock and prerollChunks=0 so legacy tests assert the
// strict pacing behaviour. Tests that exercise preroll explicitly use
// runPacingWithPreroll.
func runPacing(t *testing.T, in []StreamElement) ([]StreamElement, []time.Duration) {
	t.Helper()
	got, delays, _ := runPacingWith(t, in, newFakeClock())
	return got, delays
}

// runPacingWith runs the stage with a caller-supplied clock so tests
// can intercept sleep/now behaviour. preroll=0 disables the
// front-loaded preroll so legacy tests can exercise the strict-pacing
// math without needing to push through the preroll quota first.
func runPacingWith(
	t *testing.T,
	in []StreamElement,
	clock *fakeClock,
) ([]StreamElement, []time.Duration, error) {
	t.Helper()
	return runPacingWithPreroll(t, in, clock, 0)
}

func runPacingWithPreroll(
	t *testing.T,
	in []StreamElement,
	clock *fakeClock,
	preroll int,
) ([]StreamElement, []time.Duration, error) {
	t.Helper()

	stage := NewAudioPacingStage()
	stage.clock = clock
	stage.prerollChunks = preroll

	inCh := make(chan StreamElement, len(in))
	outCh := make(chan StreamElement, len(in))
	for _, e := range in {
		inCh <- e
	}
	close(inCh)

	err := stage.Process(context.Background(), inCh, outCh)

	var got []StreamElement
	for e := range outCh {
		got = append(got, e)
	}
	return got, clock.delays, err
}

func TestAudioPacingStage_FirstChunkForwardsImmediately(t *testing.T) {
	const sampleRate = 24000
	got, delays := runPacing(t, []StreamElement{
		audioElem(2400, sampleRate), // 50 ms of audio
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 forwarded element, got %d", len(got))
	}
	if len(delays) != 0 {
		t.Errorf("first audio chunk should not sleep, got delays %v", delays)
	}
}

func TestAudioPacingStage_SecondChunkSleepsForFirstChunkDuration(t *testing.T) {
	const sampleRate = 24000
	// 2400 bytes of s16le mono = 1200 samples = 50 ms at 24 kHz.
	got, delays := runPacing(t, []StreamElement{
		audioElem(2400, sampleRate),
		audioElem(2400, sampleRate),
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 forwarded elements, got %d", len(got))
	}
	if len(delays) != 1 {
		t.Fatalf("expected exactly one sleep, got %v", delays)
	}
	want := 50 * time.Millisecond
	if delays[0] != want {
		t.Errorf("expected exact sleep of %v (clock is fake), got %v", want, delays[0])
	}
}

func TestAudioPacingStage_NonAudioElementsPassThroughWithoutResettingClock(t *testing.T) {
	const sampleRate = 24000

	// audio, then a non-audio element (transcript delta — must NOT
	// reset the clock), then another audio chunk (which SHOULD incur a
	// sleep because the audio sequence is still in progress). This
	// guards against the bug where interleaved transcript chunks
	// re-armed preroll mid-utterance and let downstream consumers
	// accumulate seconds of buffered audio.
	textCopy := "hello"
	got, delays := runPacing(t, []StreamElement{
		audioElem(2400, sampleRate),
		{Text: &textCopy},
		audioElem(2400, sampleRate),
	})
	if len(got) != 3 {
		t.Fatalf("expected 3 forwarded elements, got %d", len(got))
	}
	if len(delays) != 1 {
		t.Fatalf("expected one sleep (chunk 2 paced against chunk 1), got %v", delays)
	}
	want := 50 * time.Millisecond
	if delays[0] != want {
		t.Errorf("expected sleep of %v, got %v", want, delays[0])
	}
}

// TestAudioPacingStage_EndOfStreamResetsClock verifies that the real
// turn-boundary signal (EndOfStream) resets pacer state so the next
// utterance starts fresh — preroll re-arms, and the first chunk of the
// new sequence forwards without sleeping.
func TestAudioPacingStage_EndOfStreamResetsClock(t *testing.T) {
	const sampleRate = 24000

	got, delays := runPacing(t, []StreamElement{
		audioElem(2400, sampleRate), // primes the clock
		{EndOfStream: true},         // turn boundary — resets pacer
		audioElem(2400, sampleRate), // first chunk of new turn, no sleep
	})
	if len(got) != 3 {
		t.Fatalf("expected 3 forwarded elements, got %d", len(got))
	}
	if len(delays) != 0 {
		t.Errorf("EndOfStream should reset clock, no sleep expected on next chunk, got %v", delays)
	}
}

func TestAudioPacingStage_PacingScalesWithSampleRate(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate int
		bytes      int
		wantSleep  time.Duration
	}{
		{"24kHz 50ms", 24000, 2400, 50 * time.Millisecond},
		{"16kHz 50ms", 16000, 1600, 50 * time.Millisecond},
		{"48kHz 50ms", 48000, 4800, 50 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, delays := runPacing(t, []StreamElement{
				audioElem(tc.bytes, tc.sampleRate),
				audioElem(tc.bytes, tc.sampleRate),
			})
			if len(delays) != 1 {
				t.Fatalf("expected 1 sleep, got %v", delays)
			}
			if delays[0] != tc.wantSleep {
				t.Errorf("expected exact sleep %v, got %v", tc.wantSleep, delays[0])
			}
		})
	}
}

func TestAudioPacingStage_DriftRecoveryWhenConsumerIsBehind(t *testing.T) {
	const sampleRate = 24000
	clock := newFakeClock()
	// Make the very next sleep cause time to advance much further than
	// requested — simulates a downstream stage that took longer than
	// the audio chunk's own duration to consume the previous output.
	// The pacing stage should observe that and NOT request a sleep on
	// the following chunk (delay would be negative).
	clock.onSleep = func(_ context.Context, d time.Duration) error {
		clock.t = clock.t.Add(d + 200*time.Millisecond) // 200 ms behind
		return nil
	}

	_, delays, err := runPacingWith(t, []StreamElement{
		audioElem(2400, sampleRate), // primer
		audioElem(2400, sampleRate), // sleeps 50 ms; clock jumps +250 ms
		audioElem(2400, sampleRate), // already past deadline → no sleep
	}, clock)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(delays) != 1 {
		t.Fatalf("expected exactly one sleep (the second chunk's), got %v", delays)
	}
}

func TestAudioPacingStage_ContextCancelDuringSleep(t *testing.T) {
	const sampleRate = 24000
	ctx, cancel := context.WithCancel(context.Background())

	clock := newFakeClock()
	clock.onSleep = func(_ context.Context, _ time.Duration) error {
		// Cancel the parent ctx mid-sleep and report it via the clock,
		// the same way the real time-based clock would.
		cancel()
		return ctx.Err()
	}

	stage := NewAudioPacingStage()
	stage.clock = clock
	// Disable preroll so the second chunk hits pacing immediately;
	// the test is exercising the cancel path inside sleep, not the
	// preroll behaviour.
	stage.prerollChunks = 0

	inCh := make(chan StreamElement, 2)
	outCh := make(chan StreamElement, 2)
	inCh <- audioElem(2400, sampleRate) // primer
	inCh <- audioElem(2400, sampleRate) // triggers sleep, which cancels
	close(inCh)

	err := stage.Process(ctx, inCh, outCh)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestAudioPacingStage_ReportsBehindDeadline(t *testing.T) {
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	reg := prometheus.NewRegistry()
	providers.RegisterDefaultStreamMetrics(reg, "test", nil)

	// A clock whose sleep OVER-advances past the requested delay, so the
	// chunk after a paced one lands past its deadline (consumer behind).
	clock := newFakeClock()
	clock.onSleep = func(_ context.Context, d time.Duration) error {
		clock.t = clock.t.Add(d + 100*time.Millisecond)
		return nil
	}

	const sampleRate = 24000
	// 2400 bytes = 1200 samples = 50 ms/chunk at 24 kHz. preroll=0.
	// chunk1: immediate; chunk2: sleeps 50ms then over-advances +100ms;
	// chunk3: now is 100ms past its 100ms deadline -> behind-deadline branch.
	_, _, err := runPacingWithPreroll(t, []StreamElement{
		audioElem(2400, sampleRate),
		audioElem(2400, sampleRate),
		audioElem(2400, sampleRate),
	}, clock, 0)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	m := providers.DefaultStreamMetrics()
	if got := testutil.ToFloat64(m.PacingBehindDeadlineVec().WithLabelValues("input")); got < 1 {
		t.Errorf("audio_pacing_behind_deadline_total{input} = %v, want >= 1", got)
	}
}

func TestPacingDirection(t *testing.T) {
	t.Parallel()
	if got := pacingDirection("audio-pacing"); got != "input" {
		t.Errorf("pacingDirection(audio-pacing) = %q, want input", got)
	}
	if got := pacingDirection("audio-pacing-output"); got != "output" {
		t.Errorf("pacingDirection(audio-pacing-output) = %q, want output", got)
	}
}

func TestAudioPacingStage_ContextCancelStopsForwarding(t *testing.T) {
	stage := NewAudioPacingStage()

	inCh := make(chan StreamElement, 1)
	outCh := make(chan StreamElement) // unbuffered → blocks forwarding

	inCh <- audioElem(2400, 24000)
	close(inCh)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	err := stage.Process(ctx, inCh, outCh)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected ctx.Canceled, got %v", err)
	}
}

// TestAudioPacingStage_PrerollChunksForwardImmediately verifies the
// preroll behaviour: the first prerollChunks audio chunks of a sequence
// forward without sleeping, building a buffer at downstream consumers
// to absorb scheduler jitter. Pacing kicks in for the chunk after.
func TestAudioPacingStage_PrerollChunksForwardImmediately(t *testing.T) {
	const sampleRate = 24000
	const preroll = 3
	clock := newFakeClock()

	in := []StreamElement{
		audioElem(2400, sampleRate),
		audioElem(2400, sampleRate),
		audioElem(2400, sampleRate),
		audioElem(2400, sampleRate), // chunk 4 should be paced
		audioElem(2400, sampleRate), // chunk 5 should be paced
	}
	got, delays, err := runPacingWithPreroll(t, in, clock, preroll)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 forwarded elements, got %d", len(got))
	}
	if len(delays) != 2 {
		t.Fatalf("expected exactly 2 sleeps (chunks 4 and 5), got %v", delays)
	}
	want := 50 * time.Millisecond
	for i, d := range delays {
		if d != want {
			t.Errorf("delay[%d] = %v, want %v", i, d, want)
		}
	}
}

// TestAudioPacingStage_EndOfStreamResetsPreroll verifies that the
// EndOfStream signal (the real turn boundary) clears the preroll
// counter so the next utterance gets fresh preroll headroom.
func TestAudioPacingStage_EndOfStreamResetsPreroll(t *testing.T) {
	const sampleRate = 24000
	const preroll = 2
	clock := newFakeClock()

	in := []StreamElement{
		audioElem(2400, sampleRate), // turn 1, preroll
		audioElem(2400, sampleRate), // turn 1, preroll
		audioElem(2400, sampleRate), // turn 1, paced (1 sleep)
		{EndOfStream: true},         // turn boundary — resets pacer
		audioElem(2400, sampleRate), // turn 2, preroll
		audioElem(2400, sampleRate), // turn 2, preroll
		audioElem(2400, sampleRate), // turn 2, paced (1 sleep)
	}
	_, delays, err := runPacingWithPreroll(t, in, clock, preroll)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(delays) != 2 {
		t.Fatalf("expected 2 sleeps total (one per turn), got %v", delays)
	}
}

// TestAudioPacingStage_InterleavedTextDoesNotRearmPreroll guards
// against the OpenAI Realtime overlap regression: a transcript-text
// chunk interleaved mid-utterance must not reset the preroll counter,
// otherwise the next 3 audio chunks would forward immediately and the
// downstream consumer (browser via SSE) accumulates seconds of buffered
// audio that overlap with the next turn's user audio.
func TestAudioPacingStage_InterleavedTextDoesNotRearmPreroll(t *testing.T) {
	const sampleRate = 24000
	const preroll = 2
	clock := newFakeClock()

	textCopy := "transcript delta"
	in := []StreamElement{
		audioElem(2400, sampleRate), // preroll
		audioElem(2400, sampleRate), // preroll
		audioElem(2400, sampleRate), // paced (1 sleep)
		{Text: &textCopy},           // transcript delta — must NOT reset preroll
		audioElem(2400, sampleRate), // paced (1 sleep), not a fresh preroll forward
		audioElem(2400, sampleRate), // paced (1 sleep)
	}
	_, delays, err := runPacingWithPreroll(t, in, clock, preroll)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(delays) != 3 {
		t.Fatalf("expected 3 sleeps (preroll counts as 2, then 3 paced chunks), got %v", delays)
	}
}

func TestAudioPacingStage_UnknownFormatForwardsImmediately(t *testing.T) {
	// An Opus chunk has no fixed bytes-per-sample on the wire, so
	// chunkDurationFor returns 0 and the stage should forward without
	// pacing — even on the second chunk.
	const sampleRate = 48000
	opus := func() StreamElement {
		return StreamElement{
			Audio: &AudioData{
				Samples:    make([]byte, 320),
				SampleRate: sampleRate,
				Channels:   1,
				Format:     AudioFormatOpus,
			},
		}
	}
	got, delays := runPacing(t, []StreamElement{opus(), opus(), opus()})
	if len(got) != 3 {
		t.Fatalf("expected 3 elements forwarded, got %d", len(got))
	}
	if len(delays) != 0 {
		t.Errorf("Opus chunks should not be paced (no fixed bps), got %v", delays)
	}
}
