package cue

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
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const cueTestRate = 24000

const brunoText = "We don't talk about Bruno."

// testCueBackchannel is a preemptible cue kind no source emits yet: a short
// acknowledgment played while the caller speaks, which gives way to reply audio.
const testCueBackchannel core.CueKind = "backchannel"

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

func pcmClip(pcm []byte, rate int) Clip {
	return Clip{Audio: &core.AudioData{Samples: pcm, SampleRate: rate, Channels: 1, Format: core.AudioFormatPCM16}}
}

// guardrailCueElem is the element a provider stage emits for a guardrail's
// cut: the message as text marked safety, carrying the cue.
func guardrailCueElem(text string) core.StreamElement {
	fr := types.FinishReasonSafety
	e := core.StreamElement{Text: &text, Cue: &core.Cue{Kind: core.CueGuardrailMessage, Text: text}}
	e.Meta.FinishReason = &fr
	return e
}

// processor is the part of a Stage a chain needs.
type processor interface {
	Process(ctx context.Context, input <-chan core.StreamElement, output chan<- core.StreamElement) error
}

// stageChain runs stages connected in order.
type stageChain struct {
	in   chan core.StreamElement
	out  chan core.StreamElement
	errs chan error
	n    int
}

func startChain(t *testing.T, stages ...processor) *stageChain {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &stageChain{in: make(chan core.StreamElement, 64), errs: make(chan error, len(stages)), n: len(stages)}
	in := c.in
	for _, s := range stages {
		out := make(chan core.StreamElement, 64)
		go func(s processor, in <-chan core.StreamElement, out chan core.StreamElement) {
			c.errs <- s.Process(ctx, in, out)
		}(s, in, out)
		in = out
	}
	c.out = in
	return c
}

// next is the chain's next output element.
func (c *stageChain) next(t *testing.T) core.StreamElement {
	t.Helper()
	select {
	case e, ok := <-c.out:
		require.True(t, ok, "the chain closed its output early")
		return e
	case <-time.After(3 * time.Second):
		require.FailNow(t, "no element came out")
		return core.StreamElement{}
	}
}

// maxChainOutput is more elements than any test's chain emits.
const maxChainOutput = 1000

// finish closes the input and returns everything still to come out. It
// gives the chain 3 seconds in all, not per element, and fails at once past
// maxChainOutput elements, so a stage that never stops emitting fails the
// test instead of hanging it.
func (c *stageChain) finish(t *testing.T) []core.StreamElement {
	t.Helper()
	close(c.in)
	deadline := time.After(3 * time.Second)
	var got []core.StreamElement
	for {
		select {
		case e, ok := <-c.out:
			if !ok {
				for range c.n {
					require.NoError(t, <-c.errs)
				}
				return got
			}
			got = append(got, e)
			if len(got) > maxChainOutput {
				require.FailNow(t, "the chain kept emitting", "more than %d elements", maxChainOutput)
			}
		case <-deadline:
			require.FailNow(t, "the chain never finished")
			return nil
		}
	}
}

// cueAudioOf concatenates the samples of the cue audio elements in elems.
func cueAudioOf(elems []core.StreamElement) []byte {
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
	cue := NewStage(Config{Clips: map[string]Clip{brunoText: fromCached(clip)}})
	c := startChain(t, cue)
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)

	require.Len(t, got, 4)
	require.NotNil(t, got[0].Text)
	assert.Equal(t, brunoText, *got[0].Text)
	assert.Equal(t, &core.Cue{Kind: core.CueGuardrailMessage, Text: brunoText}, got[0].Cue)
	assert.Nil(t, got[0].Audio)

	var sizes []int
	var durs []time.Duration
	for _, e := range got[1:] {
		require.NotNil(t, e.Audio)
		sizes = append(sizes, len(e.Audio.Samples))
		durs = append(durs, e.Audio.Duration)
		assert.Equal(t, &core.Cue{Kind: core.CueGuardrailMessage, Text: brunoText}, e.Meta.CueAudio)
		assert.Equal(t, cueTestRate, e.Audio.SampleRate)
		assert.Equal(t, 1, e.Audio.Channels)
		assert.Equal(t, core.AudioFormatPCM16, e.Audio.Format)
		assert.Equal(t, core.PriorityHigh, e.Priority)
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
	cue := NewStage(Config{Clips: map[string]Clip{brunoText: pcmClip(pcm, cueTestRate)},
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
	replyAt := func(rate int) core.StreamElement {
		return core.StreamElement{Audio: &core.AudioData{Samples: make([]byte, 320), SampleRate: rate, Channels: 1,
			Format: core.AudioFormatPCM16}}
	}
	cases := []struct {
		name     string
		clip     Clip
		before   []core.StreamElement
		wantRate int
		want     []byte
	}{
		{"no reply audio yet: 24 kHz", pcmClip(core16, 16000), nil, 24000,
			cueFaded(resampled(core16, 16000, 24000), 24000)},
		{"the first reply audio's rate", pcmClip(core24, 24000), []core.StreamElement{replyAt(16000), replyAt(24000)},
			16000, cueFaded(resampled(core24, 24000, 16000), 16000)},
		{"silence trimmed", pcmClip(padded, 24000), nil, 24000, cueFaded(core24, 24000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cue := NewStage(Config{Clips: map[string]Clip{brunoText: tc.clip}})
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
	cue := NewStage(Config{Clips: map[string]Clip{"another": pcmClip(cueRamp(2400), cueTestRate)}})
	c := startChain(t, cue)
	c.in <- guardrailCueElem(brunoText)
	got := c.finish(t)
	require.Len(t, got, 1)
	assert.Equal(t, brunoText, *got[0].Text)
}

// pendingRender is a render that finishes when released, or fails when its
// context ends.
func pendingRender(clip Clip, release <-chan struct{}) func(context.Context, string) (Clip, error) {
	return func(ctx context.Context, _ string) (Clip, error) {
		select {
		case <-release:
			return clip, nil
		case <-ctx.Done():
			return Clip{}, ctx.Err()
		}
	}
}

// replyAudioOf is a reply audio element carrying pcm at the test rate.
func replyAudioOf(pcm []byte) core.StreamElement {
	return core.StreamElement{Audio: &core.AudioData{Samples: pcm, SampleRate: cueTestRate, Channels: 1,
		Format: core.AudioFormatPCM16}}
}

// storedTexts records each text the bank stores a rendered clip for.
func storedTexts(s *CueStage) <-chan string {
	ch := make(chan string, 8)
	s.bank.stored = func(text string, _ Clip) { ch <- text }
	return ch
}

// replyEnd is a completed reply's turn-end, as the provider stage emits it.
func replyEnd(text string) core.StreamElement {
	return core.StreamElement{EndOfStream: true, Message: &types.Message{Role: core.RoleAssistant, Content: text,
		FinishReason: "stop", Meta: map[string]interface{}{"finish_reason": "stop"}}}
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

// A clip rendered once is ready for the next cue with the same text.
func TestCueStage_RenderedClipIsKept(t *testing.T) {
	pcm := cueRamp(2400)
	renders := 0
	cue := NewStage(Config{})
	cue.render = func(context.Context, string) (Clip, error) {
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
	cue := NewStage(Config{})
	cue.render = pendingRender(pcmClip(cueRamp(2400), cueTestRate), release)
	stored := storedTexts(cue)
	c := startChain(t, cue)

	before, after := cueRamp(100), cueRamp(200)
	c.in <- guardrailCueElem(brunoText)
	c.in <- guardrailCueElem("Second.") // its clip queues behind the first's
	c.in <- replyAudioOf(before)
	c.in <- replyEnd("Hi.")
	c.in <- core.NewInterruptElement()
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
	cue := NewStage(Config{OnDemandBudget: 5 * time.Millisecond})
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	cue.render = func(context.Context, string) (Clip, error) {
		<-stuck
		return Clip{}, errors.New("abandoned")
	}
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, reply, c.next(t).Audio.Samples)
	assert.Empty(t, c.finish(t))
}

// Input that closes while a clip renders past its budget: the clip plays
// nothing, what waited behind it goes on, and the stage ends.
func TestCueStage_InputCloseThenBudgetReleasesWhatWaited(t *testing.T) {
	cue := NewStage(Config{OnDemandBudget: 100 * time.Millisecond})
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	cue.render = func(context.Context, string) (Clip, error) {
		<-stuck
		return Clip{}, errors.New("abandoned")
	}
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	got := c.finish(t) // closes the input while the clip still renders
	require.Len(t, got, 1, "the reply's audio, and no clip")
	assert.Nil(t, got[0].Meta.CueAudio)
	assert.Equal(t, reply, got[0].Audio.Samples)
}

// A render past its budget still keeps its clip: the next cue with that text
// plays it at once, with no second render.
func TestCueStage_LateRenderIsKeptForNextTime(t *testing.T) {
	pcm := cueRamp(2400)
	release := make(chan struct{})
	renders := 0
	cue := NewStage(Config{OnDemandBudget: 5 * time.Millisecond})
	cue.render = func(context.Context, string) (Clip, error) {
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
	cue := NewStage(Config{OnDemandBudget: time.Minute, RenderTimeout: 5 * time.Millisecond})
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
	cue := NewStage(Config{OnDemandBudget: time.Minute})
	cue.render = pendingRender(pcmClip(cueRamp(2400), cueTestRate), release)
	stored := storedTexts(cue)
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- core.StreamElement{Cue: &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
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
	cue := NewStage(Config{})
	cue.render = pendingRender(pcmClip(pcm, cueTestRate), release)
	c := startChain(t, cue)

	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	close(c.in)
	close(release)
	var got []core.StreamElement
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

// A text cued again while the render a cut abandoned is still running waits
// for that render instead of starting a second one, and plays its clip.
func TestCueStage_RecuedTextJoinsItsRunningRender(t *testing.T) {
	pcm := cueRamp(2400)
	release := make(chan struct{})
	var mu sync.Mutex
	renders := 0
	cue := NewStage(Config{OnDemandBudget: time.Minute})
	cue.render = func(ctx context.Context, _ string) (Clip, error) {
		mu.Lock()
		renders++
		mu.Unlock()
		return pendingRender(pcmClip(pcm, cueTestRate), release)(ctx, "")
	}
	c := startChain(t, cue)

	c.in <- guardrailCueElem(brunoText)
	assert.Equal(t, brunoText, *c.next(t).Text)
	c.in <- core.NewInterruptElement() // drops the first cue's playback
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
	cue := NewStage(Config{OnDemandBudget: time.Minute,
		Clips: map[string]Clip{"mm-hm": pcmClip(cueRamp(4800), cueTestRate)}})
	cue.render = pendingRender(pcmClip(guard, cueTestRate), release)
	c := startChain(t, cue)

	backchannel := core.StreamElement{Cue: &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	reply := cueRamp(100)
	c.in <- guardrailCueElem(brunoText)
	c.in <- backchannel
	c.in <- replyAudioOf(reply)
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, "mm-hm", c.next(t).Cue.Text)

	close(release)
	got := c.finish(t)
	require.Len(t, got, 2)
	assert.Equal(t, &core.Cue{Kind: core.CueGuardrailMessage, Text: brunoText}, got[0].Meta.CueAudio)
	assert.Equal(t, cueFaded(guard, cueTestRate), got[0].Audio.Samples)
	assert.Nil(t, got[1].Meta.CueAudio)
	assert.Equal(t, reply, got[1].Audio.Samples)
}

// With no reply audio waiting, a preemptible cue queued behind a rendering
// clip still plays.
func TestCueStage_PreemptibleCueBehindPendingPlaysWithoutReplyAudio(t *testing.T) {
	guard, hm := cueRamp(2400), cueRamp(2400)
	release := make(chan struct{})
	cue := NewStage(Config{OnDemandBudget: time.Minute,
		Clips: map[string]Clip{"mm-hm": pcmClip(hm, cueTestRate)}})
	cue.render = pendingRender(pcmClip(guard, cueTestRate), release)
	c := startChain(t, cue)

	c.in <- guardrailCueElem(brunoText)
	c.in <- core.StreamElement{Cue: &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	assert.Equal(t, brunoText, *c.next(t).Text)
	assert.Equal(t, "mm-hm", c.next(t).Cue.Text)
	close(release)
	got := c.finish(t)
	require.Len(t, got, 2)
	assert.Equal(t, cueFaded(guard, cueTestRate), got[0].Audio.Samples)
	assert.Equal(t, "mm-hm", got[1].Meta.CueAudio.Text)
	assert.Equal(t, cueFaded(hm, cueTestRate), got[1].Audio.Samples)
}
