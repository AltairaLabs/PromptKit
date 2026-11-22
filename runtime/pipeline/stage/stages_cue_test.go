package stage

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const cueTestRate = 24000

const brunoText = "We don't talk about Bruno."

// testCueBackchannel is a preemptible cue kind no source emits yet: a short
// acknowledgment played while the caller speaks, which gives way to reply audio.
const testCueBackchannel CueKind = "backchannel"

// cueRamp is n samples of PCM16 mono that never fall to silence, so no byte of
// it is trimmed and every byte is distinct from a neighbor's.
func cueRamp(n int) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(1000+i%700)) //nolint:gosec // small positive values
	}
	return b
}

// cueFaded is what a cue stage plays of pcm, a clip already at rate with no
// silence at either end: pcm with its last cueFadeOut faded to silence.
func cueFaded(pcm []byte, rate int) []byte {
	n := rate * int(cueFadeOut/time.Millisecond) / 1000
	split := len(pcm) - 2*n
	out := append([]byte(nil), pcm[:split]...)
	return append(out, audio.FadeOutPCM16(pcm[split:], n)...)
}

func pcmClip(pcm []byte, rate int) cueClip {
	return cueClip{Audio: &AudioData{Samples: pcm, SampleRate: rate, Channels: 1, Format: AudioFormatPCM16}}
}

// guardrailCueElem is the element a provider stage emits for a guardrail's
// cut: the message as text marked safety, carrying the cue.
func guardrailCueElem(text string) StreamElement {
	fr := types.FinishReasonSafety
	e := StreamElement{Text: &text, Cue: &Cue{Kind: CueGuardrailMessage, Text: text}}
	e.Meta.FinishReason = &fr
	return e
}

// processor is the part of a Stage a chain needs.
type processor interface {
	Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error
}

// stageChain runs stages connected in order.
type stageChain struct {
	in   chan StreamElement
	out  chan StreamElement
	errs chan error
	n    int
}

func startChain(t *testing.T, stages ...processor) *stageChain {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &stageChain{in: make(chan StreamElement, 64), errs: make(chan error, len(stages)), n: len(stages)}
	in := c.in
	for _, s := range stages {
		out := make(chan StreamElement, 64)
		go func(s processor, in <-chan StreamElement, out chan StreamElement) {
			c.errs <- s.Process(ctx, in, out)
		}(s, in, out)
		in = out
	}
	c.out = in
	return c
}

// next is the chain's next output element.
func (c *stageChain) next(t *testing.T) StreamElement {
	t.Helper()
	select {
	case e, ok := <-c.out:
		require.True(t, ok, "the chain closed its output early")
		return e
	case <-time.After(3 * time.Second):
		require.FailNow(t, "no element came out")
		return StreamElement{}
	}
}

// finish closes the input and returns everything still to come out.
func (c *stageChain) finish(t *testing.T) []StreamElement {
	t.Helper()
	close(c.in)
	var got []StreamElement
	for {
		select {
		case e, ok := <-c.out:
			if !ok {
				for i := 0; i < c.n; i++ {
					require.NoError(t, <-c.errs)
				}
				return got
			}
			got = append(got, e)
		case <-time.After(3 * time.Second):
			require.FailNow(t, "the chain never finished")
			return nil
		}
	}
}

// outputPacer is a flush-on-cut output pacer on clock, without preroll.
func outputPacer(clock pacingClock) *AudioPacingStage {
	p := NewNamedAudioPacingStage("audio-pacing-output").WithFlushOnCut()
	p.clock = clock
	p.prerollChunks = 0
	return p
}

// blockingClock is a pacing clock whose sleeps last until the pacer gives
// them up, as a real sleep does while the audio ahead plays.
func blockingClock() *fakeClock {
	c := newFakeClock()
	c.onSleep = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	return c
}

// cueAudioOf concatenates the samples of the cue audio elements in elems.
func cueAudioOf(elems []StreamElement) []byte {
	var b []byte
	for _, e := range elems {
		if e.Audio != nil && e.Meta.CueAudio != nil {
			b = append(b, e.Audio.Samples...)
		}
	}
	return b
}

// A cue element goes on unchanged and is followed by the clip, played as
// audio elements of ChunkDuration each (the last shorter), marked as the
// cue's audio, in the reply audio's format, with the last stretch faded out.
func TestCueStage_PlaysTheClipAfterItsElement(t *testing.T) {
	pcm := fixtureBruno.pcm() // 250 ms at 24 kHz, read from the checked-in clip
	clip, ok, err := cueFixtureCache(t).Get(context.Background(), fixtureBruno.key())
	require.NoError(t, err)
	require.True(t, ok)
	cue := newCueStage(cueConfig{Clips: map[string]cueClip{brunoText: fromCached(clip)}})
	c := startChain(t, cue, outputPacer(newFakeClock()))
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)

	require.Len(t, got, 4)
	require.NotNil(t, got[0].Text)
	assert.Equal(t, brunoText, *got[0].Text)
	assert.Equal(t, &Cue{Kind: CueGuardrailMessage, Text: brunoText}, got[0].Cue)
	assert.Nil(t, got[0].Audio)

	var sizes []int
	var durs []time.Duration
	for _, e := range got[1:] {
		require.NotNil(t, e.Audio)
		sizes = append(sizes, len(e.Audio.Samples))
		durs = append(durs, e.Audio.Duration)
		assert.Equal(t, &Cue{Kind: CueGuardrailMessage, Text: brunoText}, e.Meta.CueAudio)
		assert.Equal(t, cueTestRate, e.Audio.SampleRate)
		assert.Equal(t, 1, e.Audio.Channels)
		assert.Equal(t, AudioFormatPCM16, e.Audio.Format)
		assert.Equal(t, PriorityHigh, e.Priority)
		assert.False(t, e.Timestamp.IsZero(), "recording sorts by the timestamp")
		assert.Nil(t, e.Text)
		assert.Nil(t, e.Cue, "the audio does not ask for another clip")
	}
	assert.Equal(t, []int{4800, 4800, 2400}, sizes)
	assert.Equal(t, []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 50 * time.Millisecond}, durs)
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(got))
}

// ChunkDuration sets the size of the audio elements.
func TestCueStage_ChunkDuration(t *testing.T) {
	pcm := cueRamp(6000)
	cue := newCueStage(cueConfig{Clips: map[string]cueClip{brunoText: pcmClip(pcm, cueTestRate)},
		ChunkDuration: 200 * time.Millisecond})
	c := startChain(t, cue)
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)
	var sizes []int
	for _, e := range got[1:] {
		sizes = append(sizes, len(e.Audio.Samples))
	}
	assert.Equal(t, []int{9600, 2400}, sizes)
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(got))
}

// A clip is played at the reply audio's rate (the first reply audio seen,
// else 24 kHz), resampled once, with leading and trailing silence trimmed.
func TestCueStage_PlaysInTheReplyFormat(t *testing.T) {
	core16 := cueRamp(3200) // 200 ms at 16 kHz
	core24 := cueRamp(4800) // 200 ms at 24 kHz
	silence := make([]byte, 2*800)
	padded := append(append(append([]byte(nil), silence...), core24...), silence...)
	resampled := func(pcm []byte, from, to int) []byte {
		out, err := audio.ResamplePCM16(pcm, from, to)
		require.NoError(t, err)
		return out
	}
	replyAt := func(rate int) StreamElement {
		return StreamElement{Audio: &AudioData{Samples: make([]byte, 320), SampleRate: rate, Channels: 1,
			Format: AudioFormatPCM16}}
	}
	cases := []struct {
		name     string
		clip     cueClip
		before   []StreamElement
		wantRate int
		want     []byte
	}{
		{"no reply audio yet: 24 kHz", pcmClip(core16, 16000), nil, 24000,
			cueFaded(resampled(core16, 16000, 24000), 24000)},
		{"the first reply audio's rate", pcmClip(core24, 24000), []StreamElement{replyAt(16000), replyAt(24000)},
			16000, cueFaded(resampled(core24, 24000, 16000), 16000)},
		{"silence trimmed", pcmClip(padded, 24000), nil, 24000, cueFaded(core24, 24000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cue := newCueStage(cueConfig{Clips: map[string]cueClip{brunoText: tc.clip}})
			c := startChain(t, cue)
			for _, e := range tc.before {
				c.in <- e
			}
			c.in <- guardrailCueElem(brunoText)
			got := c.finish(t)
			require.Greater(t, len(got), len(tc.before)+1, "the clip played")
			for _, e := range got[len(tc.before)+1:] {
				assert.Equal(t, tc.wantRate, e.Audio.SampleRate)
			}
			assert.Equal(t, tc.want, cueAudioOf(got))
		})
	}
}

// A cue with no clip for its text plays nothing: its text is already out.
func TestCueStage_NoClipPlaysNothing(t *testing.T) {
	cue := newCueStage(cueConfig{Clips: map[string]cueClip{"another": pcmClip(cueRamp(2400), cueTestRate)}})
	c := startChain(t, cue)
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)
	require.Len(t, got, 1)
	assert.Equal(t, brunoText, *got[0].Text)
}

// A cut after the cue (a barge-in) drops what has not played of the clip,
// like a reply's audio, and the next reply's audio plays.
func TestCueStage_CutDropsTheClip(t *testing.T) {
	cuts := map[string]StreamElement{
		"interrupt":   NewInterruptElement(),
		"interrupted": {Meta: ElementMetadata{Interrupted: true}},
	}
	for name, cut := range cuts {
		t.Run(name, func(t *testing.T) {
			pcm := cueRamp(12000) // 500 ms: five chunks
			cue := newCueStage(cueConfig{Clips: map[string]cueClip{brunoText: pcmClip(pcm, cueTestRate)}})
			c := startChain(t, cue, outputPacer(blockingClock()))
			c.in <- guardrailCueElem(brunoText)
			assert.Equal(t, brunoText, *c.next(t).Text)
			first := c.next(t)
			require.NotNil(t, first.Meta.CueAudio, "the clip's first chunk plays")
			// The second chunk now waits for the first to play out.

			next := replyAudio(100 * time.Millisecond)
			c.in <- cut
			c.in <- next
			got := c.finish(t)

			require.Len(t, got, 2, "the clip's other chunks are dropped")
			assert.True(t, core.IsCut(&got[0]))
			require.NotNil(t, got[1].Audio)
			assert.Nil(t, got[1].Meta.CueAudio)
			assert.Len(t, got[1].Audio.Samples, 4800, "the next reply's audio plays")
		})
	}
}

// pendingRender is a render that finishes when released, or fails when its
// context ends.
func pendingRender(clip cueClip, release <-chan struct{}) func(context.Context, string) (cueClip, error) {
	return func(ctx context.Context, _ string) (cueClip, error) {
		select {
		case <-release:
			return clip, nil
		case <-ctx.Done():
			return cueClip{}, ctx.Err()
		}
	}
}

// replyAudioOf is a reply audio element carrying pcm at the test rate.
func replyAudioOf(pcm []byte) StreamElement {
	return StreamElement{Audio: &AudioData{Samples: pcm, SampleRate: cueTestRate, Channels: 1,
		Format: AudioFormatPCM16}}
}

// storedTexts records each text the bank stores a rendered clip for.
func storedTexts(s *cueStage) <-chan string {
	ch := make(chan string, 8)
	s.bank.stored = func(text string, _ cueClip) { ch <- text }
	return ch
}

func waitStored(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case text := <-ch:
		return text
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the rendered clip was never stored")
		return ""
	}
}

// While a clip renders, reply output waits behind it from the first audio
// on: an element before that audio passes at once, and the reply's audio and
// the end that follows it come after the clip, in order.
func TestCueStage_PendingClipHoldsReplyOutput(t *testing.T) {
	pcm := cueRamp(6000)
	release := make(chan struct{})
	cue := newCueStage(cueConfig{})
	cue.render = pendingRender(pcmClip(pcm, cueTestRate), release)
	c := startChain(t, cue, outputPacer(newFakeClock()))

	reply := cueRamp(2400)
	user := StreamElement{Message: &types.Message{Role: core.RoleUser, Content: "Tell me about Bruno."}}
	c.in <- guardrailCueElem(brunoText)
	c.in <- user
	c.in <- replyAudioOf(reply)
	c.in <- replyEnd("Hello.")

	assert.Equal(t, brunoText, *c.next(t).Text)
	first := c.next(t)
	require.NotNil(t, first.Message, "an element ahead of the reply's audio passes while the clip renders")
	assert.Equal(t, "Tell me about Bruno.", first.Message.Content)

	close(release)
	got := c.finish(t)
	require.Len(t, got, 5, "three chunks of the clip, then the reply's audio, then its end")
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(got[:3]))
	assert.Nil(t, got[3].Meta.CueAudio)
	assert.Equal(t, reply, got[3].Audio.Samples)
	require.NotNil(t, got[4].Message)
	assert.True(t, got[4].EndOfStream)
	assert.Equal(t, "Hello.", got[4].Message.Content)
}

// Downstream of a rendering clip, a reply's end still follows its audio, so
// playout tracking holds the reply and counts its own audio only.
func TestCueStage_PendingClipKeepsReplyPlayout(t *testing.T) {
	release := make(chan struct{})
	cue := newCueStage(cueConfig{})
	cue.render = pendingRender(pcmClip(cueRamp(12000), cueTestRate), release) // 500 ms
	fc := &fakeHoldClock{t: holdT0}
	hold := NewPlayoutHoldStage()
	hold.clock = core.HoldClock{Now: fc.now, Until: fc.until}
	c := startChain(t, cue, hold)

	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudio(time.Second)
	c.in <- replyEnd("A long story.")
	assert.Equal(t, brunoText, *c.next(t).Text)
	close(release)
	var end StreamElement
	for end.Message == nil {
		end = c.next(t)
	}
	assert.True(t, end.Meta.PlayoutHeld, "the reply's end came after its audio, so its Message is held")

	fc.advance(900 * time.Millisecond) // the clip, then 400 ms of the reply
	c.in <- bargeInElem()
	assert.True(t, c.next(t).Meta.Interrupted)
	rel := c.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: types.PlayoutCutBargeIn, HeardMs: 400, AudioMs: 1000, Fraction: 0.4},
		playoutOf(rel.Message))
	c.finish(t)
}

// A clip rendered once is ready for the next cue with the same text.
func TestCueStage_RenderedClipIsKept(t *testing.T) {
	pcm := cueRamp(2400)
	renders := 0
	cue := newCueStage(cueConfig{})
	cue.render = func(context.Context, string) (cueClip, error) {
		renders++
		return pcmClip(pcm, cueTestRate), nil
	}
	c := startChain(t, cue)
	c.in <- guardrailCueElem(brunoText)
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)
	assert.Equal(t, append(cueFaded(pcm, cueTestRate), cueFaded(pcm, cueTestRate)...), cueAudioOf(got))
	assert.Equal(t, 1, renders)
}

// A cut while the clip renders drops its playback and releases what is
// queued behind it, in order, ahead of the cut. The render itself goes on and
// keeps its clip.
func TestCueStage_CutWhilePendingDropsTheCue(t *testing.T) {
	release := make(chan struct{})
	cue := newCueStage(cueConfig{})
	cue.render = pendingRender(pcmClip(cueRamp(2400), cueTestRate), release)
	stored := storedTexts(cue)
	c := startChain(t, cue)

	before, after := cueRamp(100), cueRamp(200)
	c.in <- guardrailCueElem(brunoText)
	c.in <- guardrailCueElem("Second.") // its clip queues behind the first's
	c.in <- replyAudioOf(before)
	c.in <- replyEnd("Hi.")
	c.in <- NewInterruptElement()
	c.in <- replyAudioOf(after)
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, "Second.", *c.next(t).Text)
	assert.Equal(t, before, c.next(t).Audio.Samples)
	assert.Equal(t, "Hi.", c.next(t).Message.Content)
	assert.True(t, c.next(t).Interrupt)
	assert.Equal(t, after, c.next(t).Audio.Samples)

	close(release)
	assert.Equal(t, brunoText, waitStored(t, stored), "the cut did not cancel the render")
	assert.Empty(t, cueAudioOf(c.finish(t)), "the cut cue never plays")
}

// A clip not ready within OnDemandBudget plays nothing, and what waited
// behind it goes on, even when the render never returns.
func TestCueStage_HoldBudgetReleasesTheAudio(t *testing.T) {
	cue := newCueStage(cueConfig{OnDemandBudget: 5 * time.Millisecond})
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	cue.render = func(context.Context, string) (cueClip, error) {
		<-stuck
		return cueClip{}, errors.New("abandoned")
	}
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, reply, c.next(t).Audio.Samples)
	assert.Empty(t, c.finish(t))
}

// A render past its budget still keeps its clip: the next cue with that text
// plays it at once, with no second render.
func TestCueStage_LateRenderIsKeptForNextTime(t *testing.T) {
	pcm := cueRamp(2400)
	release := make(chan struct{})
	renders := 0
	cue := newCueStage(cueConfig{OnDemandBudget: 5 * time.Millisecond})
	cue.render = func(context.Context, string) (cueClip, error) {
		renders++
		<-release
		return pcmClip(pcm, cueTestRate), nil
	}
	stored := storedTexts(cue)
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, reply, c.next(t).Audio.Samples, "the budget ran out")

	close(release)
	assert.Equal(t, brunoText, waitStored(t, stored))
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(got))
	assert.Equal(t, 1, renders)
}

// RenderTimeout bounds the render itself: a render that has not finished by
// then is given up, and what waited goes on without the clip.
func TestCueStage_RenderTimeoutEndsTheRender(t *testing.T) {
	cue := newCueStage(cueConfig{OnDemandBudget: time.Minute, RenderTimeout: 5 * time.Millisecond})
	cue.render = pendingRender(pcmClip(cueRamp(2400), cueTestRate), make(chan struct{}))
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, reply, c.next(t).Audio.Samples)
	assert.Empty(t, c.finish(t))
}

// Reply audio does not wait for a preemptible cue's clip still rendering: the
// cue gives way, and its clip does not play when the render finishes.
func TestCueStage_PendingPreemptibleCueDoesNotHoldReplyAudio(t *testing.T) {
	release := make(chan struct{})
	cue := newCueStage(cueConfig{OnDemandBudget: time.Minute})
	cue.render = pendingRender(pcmClip(cueRamp(2400), cueTestRate), release)
	stored := storedTexts(cue)
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- StreamElement{Cue: &Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	c.in <- replyAudioOf(reply)
	assert.NotNil(t, c.next(t).Cue)
	assert.Equal(t, reply, c.next(t).Audio.Samples, "the reply's audio passes at once")

	close(release)
	assert.Equal(t, "mm-hm", waitStored(t, stored))
	assert.Empty(t, c.finish(t), "the late clip is not played")
}

// Input that closes while a clip renders still gets the clip, then what
// waited behind it.
func TestCueStage_InputCloseWaitsForRenderingClip(t *testing.T) {
	pcm := cueRamp(2400)
	release := make(chan struct{})
	cue := newCueStage(cueConfig{})
	cue.render = pendingRender(pcmClip(pcm, cueTestRate), release)
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	close(c.in)
	close(release)
	var got []StreamElement
	for e := range c.out {
		got = append(got, e)
	}
	for i := 0; i < c.n; i++ {
		require.NoError(t, <-c.errs)
	}
	require.Len(t, got, 2)
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(got[:1]))
	assert.Equal(t, reply, got[1].Audio.Samples)
}

// stepClock is a pacing clock whose sleeps report that they started and last
// until tick is closed or the pacer gives them up.
type stepClock struct {
	t        time.Time
	sleeping chan struct{}
	tick     chan struct{}
}

func (c *stepClock) now() time.Time { return c.t }

func (c *stepClock) sleep(ctx context.Context, d time.Duration) error {
	select {
	case c.sleeping <- struct{}{}:
	default:
	}
	select {
	case <-c.tick:
		c.t = c.t.Add(d)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A preemptible cue's audio still queued at the pacer is dropped when reply
// audio arrives; a guardrail cue's is not.
func TestCueStage_PreemptibleCueGivesWayToReplyAudio(t *testing.T) {
	cases := []struct {
		name       string
		cue        StreamElement
		wantChunks int // of the clip's 4, after the first
	}{
		{"backchannel", StreamElement{Cue: &Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}, 0},
		{"guardrail", guardrailCueElem("mm-hm"), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pcm := cueRamp(9600) // 400 ms: four chunks
			cue := newCueStage(cueConfig{Clips: map[string]cueClip{"mm-hm": pcmClip(pcm, cueTestRate)}})
			src := startChain(t, cue)
			src.in <- tc.cue
			played := src.finish(t)
			require.Len(t, played, 5)

			clock := &stepClock{t: time.Unix(0, 0), sleeping: make(chan struct{}, 1), tick: make(chan struct{})}
			in, out := make(chan StreamElement), make(chan StreamElement, 64)
			done := make(chan error, 1)
			go func() { done <- outputPacer(clock).Process(context.Background(), in, out) }()
			for _, e := range played {
				in <- e
			}
			assert.Equal(t, tc.cue.Cue, (<-out).Cue)
			assert.NotNil(t, (<-out).Meta.CueAudio, "the first chunk plays")
			<-clock.sleeping // the second waits for it

			reply := cueRamp(240)
			marker := "marker"
			in <- StreamElement{Audio: &AudioData{Samples: reply, SampleRate: cueTestRate, Channels: 1}}
			in <- StreamElement{Text: &marker} // the pacer has read the reply audio
			if tc.wantChunks == 0 {
				// The preempted chunk stops waiting at once, and the reply starts
				// its own sequence: it goes out with no wait at all.
				for _, want := range []string{"reply", "marker"} {
					select {
					case e := <-out:
						if want == "reply" {
							assert.Equal(t, reply, e.Audio.Samples)
						} else {
							assert.Equal(t, marker, *e.Text)
						}
					case <-time.After(3 * time.Second):
						require.FailNow(t, "the preempted cue kept the reply waiting")
					}
				}
				close(in)
				require.NoError(t, <-done)
				assert.Empty(t, out)
				return
			}
			close(clock.tick)
			close(in)
			require.NoError(t, <-done)

			var got []StreamElement
			for e := range out {
				got = append(got, e)
			}
			require.Len(t, got, tc.wantChunks+2)
			for _, e := range got[:tc.wantChunks] {
				assert.NotNil(t, e.Meta.CueAudio)
			}
			assert.Equal(t, reply, got[tc.wantChunks].Audio.Samples, "the reply's audio plays")
			assert.Equal(t, marker, *got[tc.wantChunks+1].Text)
		})
	}
}

// Cue audio plays out before a held Message is released, but it is not the
// reply's audio: a reply after a cue reports its own audio only.
func TestCueStage_PlayoutHoldKeepsCueAudioOutOfTheReply(t *testing.T) {
	r := startHold(t)
	cueAudio := replyAudio(500 * time.Millisecond)
	cueAudio.Meta.CueAudio = &Cue{Kind: CueGuardrailMessage, Text: brunoText}
	r.pass(t, cueAudio, replyAudio(time.Second), replyEnd("A long story."))

	r.clock.advance(900 * time.Millisecond) // the cue, then 400 ms of the reply
	r.pass(t, bargeInElem())
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: types.PlayoutCutBargeIn, HeardMs: 400, AudioMs: 1000, Fraction: 0.4},
		playoutOf(rel.Message))
	close(r.in)
	require.NoError(t, <-r.done)
}

// A held Message waits for cue audio ahead of it to play out.
func TestCueStage_PlayoutHoldWaitsForCueAudio(t *testing.T) {
	r := startHold(t)
	cueAudio := replyAudio(500 * time.Millisecond)
	cueAudio.Meta.CueAudio = &Cue{Kind: CueGuardrailMessage, Text: brunoText}
	got := r.pass(t, replyAudio(100*time.Millisecond), cueAudio, replyEnd("Hi."))
	assert.True(t, got[2].Meta.PlayoutHeld)
	r.clock.advance(550 * time.Millisecond)
	assert.True(t, r.idle(t), "the cue is still playing")
	r.clock.advance(50 * time.Millisecond)
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, "Hi.", rel.Message.Content)
	close(r.in)
	require.NoError(t, <-r.done)
}

// A recording holds the cue's audio as assistant audio tagged with the cue's
// kind, and no message for it.
func TestCueStage_RecordingTagsCueAudio(t *testing.T) {
	pcm := cueRamp(6000)
	store := &fakeEventStore{}
	rec := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput, SessionID: "s",
		IncludeAudio: true})
	cue := newCueStage(cueConfig{Clips: map[string]cueClip{brunoText: pcmClip(pcm, cueTestRate)}})
	c := startChain(t, cue, rec)

	reply := cueRamp(480)
	c.in <- StreamElement{Audio: &AudioData{Samples: reply, SampleRate: cueTestRate, Channels: 1,
		Format: AudioFormatPCM16}, Timestamp: time.Now()}
	c.in <- guardrailCueElem(brunoText)
	end := StreamElement{EndOfStream: true, Message: &types.Message{Role: core.RoleAssistant, Content: brunoText,
		FinishReason: types.FinishReasonSafety}}
	c.in <- end
	c.finish(t)

	audioEvents := store.filterByType(events.EventAudioOutput)
	require.Len(t, audioEvents, 4)
	var kinds, from []string
	var clip []byte
	for i, e := range audioEvents {
		data := e.Data.(*events.AudioEventData)
		kinds = append(kinds, data.CueKind)
		from = append(from, data.GeneratedFrom)
		if i > 0 {
			clip = append(clip, data.Payload.InlineData...)
		}
	}
	assert.Equal(t, []string{"", "guardrail_message", "guardrail_message", "guardrail_message"}, kinds)
	assert.Equal(t, []string{"model", "cue", "cue", "cue"}, from)
	assert.Equal(t, reply, audioEvents[0].Data.(*events.AudioEventData).Payload.InlineData)
	assert.Equal(t, cueFaded(pcm, cueTestRate), clip)

	msgs := store.filterByType(events.EventMessageCreated)
	require.Len(t, msgs, 1, "no message for the cue")
	assert.Equal(t, brunoText, msgs[0].Data.(*events.MessageCreatedData).Content)
}

// An input element carrying only a cue (a backchannel a mic-side stage asks
// for) goes to the output, not to the session.
func TestDuplexProviderStage_ForwardsCueOnlyInput(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)
	elem := StreamElement{Cue: &Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	out := make(chan StreamElement, 4)
	done := make(chan error, 1)

	require.False(t, s.handleInputElement(context.Background(), &elem, out, done))
	require.Len(t, out, 1)
	assert.Equal(t, &Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}, (<-out).Cue)
	assert.Empty(t, done)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	assert.Empty(t, sess.texts)
	assert.Empty(t, sess.chunks)
}

// A text cued again while the render a cut abandoned is still running waits
// for that render instead of starting a second one, and plays its clip.
func TestCueStage_RecuedTextJoinsItsRunningRender(t *testing.T) {
	pcm := cueRamp(2400)
	release := make(chan struct{})
	var mu sync.Mutex
	renders := 0
	cue := newCueStage(cueConfig{OnDemandBudget: time.Minute})
	cue.render = func(ctx context.Context, _ string) (cueClip, error) {
		mu.Lock()
		renders++
		mu.Unlock()
		return pendingRender(pcmClip(pcm, cueTestRate), release)(ctx, "")
	}
	c := startChain(t, cue)

	c.in <- guardrailCueElem(brunoText)
	assert.Equal(t, brunoText, *c.next(t).Text)
	c.in <- NewInterruptElement() // drops the first cue's playback
	assert.True(t, c.next(t).Interrupt)
	c.in <- guardrailCueElem(brunoText)
	assert.Equal(t, brunoText, *c.next(t).Text)

	close(release)
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(c.finish(t)), "the second cue plays the clip")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, renders)
}

// A preemptible cue queued behind a guardrail cue whose clip renders gives
// way to the reply audio queued after it: the guardrail clip plays, then the
// reply, and the preemptible cue's clip not at all.
func TestCueStage_PreemptibleCueBehindPendingGivesWayToReplyAudio(t *testing.T) {
	guard := cueRamp(2400)
	release := make(chan struct{})
	cue := newCueStage(cueConfig{OnDemandBudget: time.Minute,
		Clips: map[string]cueClip{"mm-hm": pcmClip(cueRamp(4800), cueTestRate)}})
	cue.render = pendingRender(pcmClip(guard, cueTestRate), release)
	c := startChain(t, cue)

	backchannel := StreamElement{Cue: &Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- backchannel
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, "mm-hm", c.next(t).Cue.Text)

	close(release)
	got := c.finish(t)
	require.Len(t, got, 2)
	assert.Equal(t, &Cue{Kind: CueGuardrailMessage, Text: brunoText}, got[0].Meta.CueAudio)
	assert.Equal(t, cueFaded(guard, cueTestRate), got[0].Audio.Samples)
	assert.Nil(t, got[1].Meta.CueAudio)
	assert.Equal(t, reply, got[1].Audio.Samples)
}

// With no reply audio waiting, a preemptible cue queued behind a rendering
// clip still plays.
func TestCueStage_PreemptibleCueBehindPendingPlaysWithoutReplyAudio(t *testing.T) {
	guard, hm := cueRamp(2400), cueRamp(2400)
	release := make(chan struct{})
	cue := newCueStage(cueConfig{OnDemandBudget: time.Minute,
		Clips: map[string]cueClip{"mm-hm": pcmClip(hm, cueTestRate)}})
	cue.render = pendingRender(pcmClip(guard, cueTestRate), release)
	c := startChain(t, cue)

	c.in <- guardrailCueElem(brunoText)
	c.in <- StreamElement{Cue: &Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, "mm-hm", c.next(t).Cue.Text)
	close(release)
	got := c.finish(t)
	require.Len(t, got, 2)
	assert.Equal(t, cueFaded(guard, cueTestRate), got[0].Audio.Samples)
	assert.Equal(t, "mm-hm", got[1].Meta.CueAudio.Text)
	assert.Equal(t, cueFaded(hm, cueTestRate), got[1].Audio.Samples)
}
