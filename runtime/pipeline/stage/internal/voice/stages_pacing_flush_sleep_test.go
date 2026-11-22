package voice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
)

// sleepingPacer is a flush-on-cut output pacer, without preroll, whose sleeps
// last until the pacer gives them up, as a real sleep does while audio plays.
// sleeping receives once each sleep has begun, so a test can send what must
// arrive during one.
type sleepingPacer struct {
	in       chan core.StreamElement
	out      chan core.StreamElement
	done     chan error
	sleeping chan struct{}
	sleeps   int
}

func startSleepingPacer(t *testing.T) *sleepingPacer {
	t.Helper()
	p := &sleepingPacer{
		in:       make(chan core.StreamElement, 8),
		out:      make(chan core.StreamElement, 8),
		done:     make(chan error, 1),
		sleeping: make(chan struct{}, 8),
	}
	clock := newFakeClock()
	clock.onSleep = func(ctx context.Context, _ time.Duration) error {
		p.sleeps++
		p.sleeping <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	stage := NewNamedAudioPacingStage("audio-pacing-output").WithFlushOnCut()
	stage.clock = clock
	stage.prerollChunks = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { p.done <- stage.Process(ctx, p.in, p.out) }()
	return p
}

// forwarded closes the input and returns everything the pacer forwarded
// after the elements already received.
func (p *sleepingPacer) forwarded(t *testing.T) []core.StreamElement {
	t.Helper()
	close(p.in)
	require.NoError(t, recvWithin(t, p.done))
	var got []core.StreamElement
	for e := range p.out {
		got = append(got, e)
	}
	return got
}

func seqAudio(seq int64, cue *core.Cue) core.StreamElement {
	e := audioElem(2400, 24000) // 50 ms
	e.Sequence = seq
	e.Meta.CueAudio = cue
	return e
}

// A cut that arrives while the pacer sleeps on the cut reply's audio ends the
// sleep: the cut goes out at once and the audio it was waiting to play never
// does.
func TestAudioPacingStage_CutDuringSleepDropsTheAudioBeingWaitedOn(t *testing.T) {
	p := startSleepingPacer(t)
	p.in <- seqAudio(1, nil)
	assert.Equal(t, int64(1), recvWithin(t, p.out).Sequence, "the first chunk goes out at once")
	p.in <- seqAudio(2, nil)
	recvWithin(t, p.sleeping) // the pacer waits for chunk 1 to play before chunk 2

	cut := core.StreamElement{Interrupt: true, Sequence: 3}
	p.in <- cut

	got := p.forwarded(t)
	require.Len(t, got, 1, "only the cut, without the audio of the reply it cut")
	assert.Equal(t, int64(3), got[0].Sequence)
	assert.True(t, got[0].Interrupt)
	assert.Equal(t, 1, p.sleeps)
}

// Reply audio that arrives while the pacer sleeps on a preemptible cue's
// audio ends the sleep: the cue's audio is dropped, and the reply's audio
// starts a new sequence, so it goes out at once rather than after the cue.
func TestAudioPacingStage_ReplyAudioDuringPreemptibleCueSleepStartsANewSequence(t *testing.T) {
	cue := &core.Cue{Kind: core.CueKind("filler"), Text: "um", Preemptible: true}
	p := startSleepingPacer(t)
	p.in <- seqAudio(1, cue)
	assert.Equal(t, int64(1), recvWithin(t, p.out).Sequence, "the cue's first chunk goes out at once")
	p.in <- seqAudio(2, cue)
	recvWithin(t, p.sleeping) // the pacer waits for the cue's first chunk to play

	p.in <- seqAudio(3, nil) // the reply's first chunk

	got := p.forwarded(t)
	require.Len(t, got, 1, "the reply's chunk, without the cue's queued audio")
	assert.Equal(t, int64(3), got[0].Sequence)
	assert.Nil(t, got[0].Meta.CueAudio)
	assert.Equal(t, 1, p.sleeps, "the reply's chunk starts a new sequence: it does not wait for the cue")
}

// An element without audio (a transcript ahead of a reply's audio) goes out
// at once, unpaced, and the audio after it still starts a sequence: it goes
// out at once too.
func TestAudioPacingStage_FlushOnCutForwardsAnElementWithoutAudioAtOnce(t *testing.T) {
	p := startSleepingPacer(t)
	text := "hello"
	p.in <- core.StreamElement{Text: &text, Sequence: 1}
	got := recvWithin(t, p.out)
	assert.Equal(t, int64(1), got.Sequence)
	require.NotNil(t, got.Text)
	assert.Equal(t, "hello", *got.Text)

	p.in <- seqAudio(2, nil)
	assert.Equal(t, int64(2), recvWithin(t, p.out).Sequence)
	assert.Empty(t, p.forwarded(t))
	assert.Equal(t, 0, p.sleeps)
}

// startReleasablePacer is startSleepingPacer with an unbuffered input, so a
// send returns only once the pacer's reader has taken the element before it,
// and sleeps that also end, without error, once release is closed.
func startReleasablePacer(t *testing.T) (p *sleepingPacer, release chan struct{}) {
	t.Helper()
	p = &sleepingPacer{
		in:       make(chan core.StreamElement),
		out:      make(chan core.StreamElement, 8),
		done:     make(chan error, 1),
		sleeping: make(chan struct{}, 8),
	}
	release = make(chan struct{})
	clock := newFakeClock()
	clock.onSleep = func(ctx context.Context, _ time.Duration) error {
		p.sleeps++
		p.sleeping <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	stage := NewNamedAudioPacingStage("audio-pacing-output").WithFlushOnCut()
	stage.clock = clock
	stage.prerollChunks = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { p.done <- stage.Process(ctx, p.in, p.out) }()
	return p, release
}

// sendDuringSleep starts a sequence with first, waits until the pacer sleeps
// on second, sends during, and then a text marker, so during has been queued
// before the sleep is released. It returns everything forwarded after first.
func sendDuringSleep(t *testing.T, first, second, during core.StreamElement) []core.StreamElement {
	t.Helper()
	p, release := startReleasablePacer(t)
	p.in <- first
	assert.Equal(t, first.Sequence, recvWithin(t, p.out).Sequence)
	p.in <- second
	recvWithin(t, p.sleeping)
	p.in <- during
	marker := "marker"
	p.in <- core.StreamElement{Text: &marker, Sequence: 99}
	close(release)
	return p.forwarded(t)
}

func seqs(elems []core.StreamElement) []int64 {
	out := make([]int64, 0, len(elems))
	for _, e := range elems {
		out = append(out, e.Sequence)
	}
	return out
}

// A preemptible cue's own audio does not preempt the cue: its chunks play in
// turn.
func TestAudioPacingStage_ACuesNextChunkDoesNotPreemptTheCue(t *testing.T) {
	cue := &core.Cue{Kind: core.CueKind("filler"), Text: "um", Preemptible: true}
	got := sendDuringSleep(t, seqAudio(1, cue), seqAudio(2, cue), seqAudio(3, cue))
	assert.Equal(t, []int64{2, 3, 99}, seqs(got))
}

// Reply audio arriving while the pacer waits on earlier reply audio does not
// end the wait: only a preemptible cue's audio gives way to reply audio.
func TestAudioPacingStage_ReplyAudioDoesNotCutShortTheReplyAheadOfIt(t *testing.T) {
	got := sendDuringSleep(t, seqAudio(1, nil), seqAudio(2, nil), seqAudio(3, nil))
	assert.Equal(t, []int64{2, 3, 99}, seqs(got))
}
