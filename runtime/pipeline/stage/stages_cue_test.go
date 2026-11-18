package stage

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const cueTestRate = 24000

const brunoText = "We don't talk about Bruno."

// cueRamp is n samples of PCM16 mono that never fall to silence, so no byte of
// it is trimmed and every byte is distinct from a neighbor's.
func cueRamp(n int) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(1000+i%700)) //nolint:gosec // small positive values
	}
	return b
}

// cueFaded is what a CueStage plays of pcm, a clip already at rate with no
// silence at either end: pcm with its last cueFadeOut faded to silence.
func cueFaded(pcm []byte, rate int) []byte {
	n := rate * int(cueFadeOut/time.Millisecond) / 1000
	split := len(pcm) - 2*n
	out := append([]byte(nil), pcm[:split]...)
	return append(out, audio.FadeOutPCM16(pcm[split:], n)...)
}

func cueClip(pcm []byte, rate int) CueClip {
	return CueClip{Audio: &AudioData{Samples: pcm, SampleRate: rate, Channels: 1, Format: AudioFormatPCM16}}
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
	pcm := cueRamp(6000) // 250 ms at 24 kHz
	cue := NewCueStage(CueConfig{Clips: map[string]CueClip{brunoText: cueClip(pcm, cueTestRate)}})
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
	cue := NewCueStage(CueConfig{Clips: map[string]CueClip{brunoText: cueClip(pcm, cueTestRate)},
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
		clip     CueClip
		before   []StreamElement
		wantRate int
		want     []byte
	}{
		{"no reply audio yet: 24 kHz", cueClip(core16, 16000), nil, 24000,
			cueFaded(resampled(core16, 16000, 24000), 24000)},
		{"the first reply audio's rate", cueClip(core24, 24000), []StreamElement{replyAt(16000), replyAt(24000)},
			16000, cueFaded(resampled(core24, 24000, 16000), 16000)},
		{"silence trimmed", cueClip(padded, 24000), nil, 24000, cueFaded(core24, 24000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cue := NewCueStage(CueConfig{Clips: map[string]CueClip{brunoText: tc.clip}})
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
	cue := NewCueStage(CueConfig{Clips: map[string]CueClip{"another": cueClip(cueRamp(2400), cueTestRate)}})
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
			cue := NewCueStage(CueConfig{Clips: map[string]CueClip{brunoText: cueClip(pcm, cueTestRate)}})
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
			assert.True(t, isCut(&got[0]))
			require.NotNil(t, got[1].Audio)
			assert.Nil(t, got[1].Meta.CueAudio)
			assert.Len(t, got[1].Audio.Samples, 4800, "the next reply's audio plays")
		})
	}
}

// pendingRender is a render that finishes when released, or fails when its
// context ends.
func pendingRender(clip CueClip, release <-chan struct{}) func(context.Context, string) (CueClip, error) {
	return func(ctx context.Context, _ string) (CueClip, error) {
		select {
		case <-release:
			return clip, nil
		case <-ctx.Done():
			return CueClip{}, ctx.Err()
		}
	}
}

// While a clip renders, only audio waits behind it: a Message passes at once,
// and reply audio follows the clip once it is ready.
func TestCueStage_PendingClipHoldsOnlyAudio(t *testing.T) {
	pcm := cueRamp(6000)
	release := make(chan struct{})
	cue := NewCueStage(CueConfig{})
	cue.render = pendingRender(cueClip(pcm, cueTestRate), release)
	c := startChain(t, cue, outputPacer(newFakeClock()))

	reply := cueRamp(2400)
	c.in <- guardrailCueElem(brunoText)
	c.in <- StreamElement{Audio: &AudioData{Samples: reply, SampleRate: cueTestRate, Channels: 1,
		Format: AudioFormatPCM16}}
	c.in <- replyEnd("Hello.")

	assert.Equal(t, brunoText, *c.next(t).Text)
	end := c.next(t)
	require.NotNil(t, end.Message, "the Message passes while the clip renders")
	assert.Equal(t, "Hello.", end.Message.Content)
	assert.Nil(t, end.Audio)

	close(release)
	got := c.finish(t)
	require.Len(t, got, 4, "three chunks of the clip, then the reply's audio")
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(got[:3]))
	assert.Nil(t, got[3].Meta.CueAudio)
	assert.Equal(t, reply, got[3].Audio.Samples)
}

// A clip rendered once is ready for the next cue with the same text.
func TestCueStage_RenderedClipIsKept(t *testing.T) {
	pcm := cueRamp(2400)
	renders := 0
	cue := NewCueStage(CueConfig{})
	cue.render = func(context.Context, string) (CueClip, error) {
		renders++
		return cueClip(pcm, cueTestRate), nil
	}
	c := startChain(t, cue)
	c.in <- guardrailCueElem(brunoText)
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)
	assert.Equal(t, append(cueFaded(pcm, cueTestRate), cueFaded(pcm, cueTestRate)...), cueAudioOf(got))
	assert.Equal(t, 1, renders)
}

// A cut while the clip renders drops the cue and releases the audio queued
// behind it, in order, ahead of the cut.
func TestCueStage_CutWhilePendingDropsTheCue(t *testing.T) {
	cue := NewCueStage(CueConfig{})
	cue.render = pendingRender(cueClip(cueRamp(2400), cueTestRate), make(chan struct{}))
	c := startChain(t, cue)

	before, after := cueRamp(100), cueRamp(200)
	c.in <- guardrailCueElem(brunoText)
	c.in <- StreamElement{Audio: &AudioData{Samples: before, SampleRate: cueTestRate, Channels: 1}}
	c.in <- NewInterruptElement()
	c.in <- StreamElement{Audio: &AudioData{Samples: after, SampleRate: cueTestRate, Channels: 1}}
	got := c.finish(t)

	require.Len(t, got, 4)
	assert.Equal(t, brunoText, *got[0].Text)
	assert.Equal(t, before, got[1].Audio.Samples)
	assert.True(t, got[2].Interrupt)
	assert.Equal(t, after, got[3].Audio.Samples)
	assert.Empty(t, cueAudioOf(got), "the cut cue never plays")
}

// A clip that does not render within RenderTimeout plays nothing, and the
// audio waiting behind it goes on, even when the render ignores its context.
func TestCueStage_RenderTimeoutReleasesTheAudio(t *testing.T) {
	cue := NewCueStage(CueConfig{RenderTimeout: 5 * time.Millisecond})
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	cue.render = func(context.Context, string) (CueClip, error) {
		<-stuck
		return CueClip{}, nil
	}
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- StreamElement{Audio: &AudioData{Samples: reply, SampleRate: cueTestRate, Channels: 1}}
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, reply, c.next(t).Audio.Samples)
	assert.Empty(t, c.finish(t))
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
		{"backchannel", StreamElement{Cue: &Cue{Kind: CueBackchannel, Text: "mm-hm", Preemptible: true}}, 0},
		{"guardrail", guardrailCueElem("mm-hm"), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pcm := cueRamp(9600) // 400 ms: four chunks
			cue := NewCueStage(CueConfig{Clips: map[string]CueClip{"mm-hm": cueClip(pcm, cueTestRate)}})
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
				// The preempted chunk stops waiting at once: the next sleep is
				// already the reply's.
				select {
				case <-clock.sleeping:
				case <-time.After(3 * time.Second):
					require.FailNow(t, "the preempted chunk kept the reply waiting")
				}
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
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: PlayoutCutBargeIn, HeardMs: 400, AudioMs: 1000, Fraction: 0.4},
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
	cue := NewCueStage(CueConfig{Clips: map[string]CueClip{brunoText: cueClip(pcm, cueTestRate)}})
	c := startChain(t, cue, rec)

	reply := cueRamp(480)
	c.in <- StreamElement{Audio: &AudioData{Samples: reply, SampleRate: cueTestRate, Channels: 1,
		Format: AudioFormatPCM16}, Timestamp: time.Now()}
	c.in <- guardrailCueElem(brunoText)
	end := StreamElement{EndOfStream: true, Message: &types.Message{Role: roleAssistant, Content: brunoText,
		FinishReason: types.FinishReasonSafety}}
	c.in <- end
	c.finish(t)

	audioEvents := store.filterByType(events.EventAudioOutput)
	require.Len(t, audioEvents, 4)
	var kinds []string
	var clip []byte
	for i, e := range audioEvents {
		data := e.Data.(*events.AudioEventData)
		kinds = append(kinds, data.CueKind)
		if i > 0 {
			clip = append(clip, data.Payload.InlineData...)
		}
	}
	assert.Equal(t, []string{"", "guardrail_message", "guardrail_message", "guardrail_message"}, kinds)
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
	elem := StreamElement{Cue: &Cue{Kind: CueBackchannel, Text: "mm-hm", Preemptible: true}}
	out := make(chan StreamElement, 4)
	done := make(chan error, 1)

	require.False(t, s.handleInputElement(context.Background(), &elem, out, done))
	require.Len(t, out, 1)
	assert.Equal(t, &Cue{Kind: CueBackchannel, Text: "mm-hm", Preemptible: true}, (<-out).Cue)
	assert.Empty(t, done)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	assert.Empty(t, sess.texts)
	assert.Empty(t, sess.chunks)
}
