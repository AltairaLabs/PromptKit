package stage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// fakeHoldClock is a manual clock: time moves only on advance, which fires
// the waits it passes.
type fakeHoldClock struct {
	mu      sync.Mutex
	t       time.Time
	waiters []fakeHoldWait
}

type fakeHoldWait struct {
	at time.Time
	c  chan time.Time
}

var holdT0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func (f *fakeHoldClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeHoldClock) after(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := make(chan time.Time, 1)
	if d <= 0 {
		c <- f.t
		return c
	}
	f.waiters = append(f.waiters, fakeHoldWait{at: f.t.Add(d), c: c})
	return c
}

func (f *fakeHoldClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
	kept := f.waiters[:0]
	for _, w := range f.waiters {
		if w.at.After(f.t) {
			kept = append(kept, w)
			continue
		}
		w.c <- f.t
	}
	f.waiters = kept
}

// replyAudio is d of 24 kHz mono PCM16 audio.
func replyAudio(d time.Duration) StreamElement {
	n := int(d.Seconds()*24000) * 2
	return StreamElement{Audio: &AudioData{Samples: make([]byte, n), SampleRate: 24000, Channels: 1,
		Format: AudioFormatPCM16}}
}

// replyEnd is a completed reply's turn-end.
func replyEnd(text string) StreamElement {
	return StreamElement{EndOfStream: true, Message: &types.Message{Role: roleAssistant, Content: text,
		FinishReason: "stop", Meta: map[string]interface{}{finishReasonMetaKey: "stop"}}}
}

func bargeInElem() StreamElement {
	e := StreamElement{}
	e.Meta.Interrupted = true
	return e
}

// holdRun drives a PlayoutHoldStage on a fake clock.
type holdRun struct {
	clock  *fakeHoldClock
	in     chan StreamElement
	out    chan StreamElement
	cancel context.CancelFunc
	done   chan error
}

func startHold(t *testing.T) *holdRun {
	t.Helper()
	fc := &fakeHoldClock{t: holdT0}
	s := NewPlayoutHoldStage()
	s.clock = holdClock{now: fc.now, after: fc.after}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &holdRun{clock: fc, in: make(chan StreamElement, 16), out: make(chan StreamElement, 64),
		cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- s.Process(ctx, r.in, r.out) }()
	return r
}

// next returns the next output element (failing after a generous bound).
func (r *holdRun) next(t *testing.T) StreamElement {
	t.Helper()
	select {
	case e := <-r.out:
		return e
	case <-time.After(3 * time.Second):
		require.FailNow(t, "no element came out")
		return StreamElement{}
	}
}

// pass sends elems and reads each back as forwarded: the stage has handled
// them all once this returns.
func (r *holdRun) pass(t *testing.T, elems ...StreamElement) []StreamElement {
	t.Helper()
	var got []StreamElement
	for _, e := range elems {
		r.in <- e
		got = append(got, r.next(t))
	}
	return got
}

// idle reports that nothing is waiting on the output: a marker element sent
// now comes straight back.
func (r *holdRun) idle(t *testing.T) bool {
	t.Helper()
	marker := "marker"
	r.in <- StreamElement{Text: &marker}
	e := r.next(t)
	return e.Text != nil && *e.Text == marker
}

// playoutMeta is a released reply's playout metadata.
type playoutMeta struct {
	Finish   string
	Cut      any
	HeardMs  int64
	AudioMs  int64
	Fraction float64
}

func playoutOf(m *types.Message) playoutMeta {
	heardMs, _ := m.Meta[MetaHeardMs].(int64)
	audioMs, _ := m.Meta[MetaAudioMs].(int64)
	fraction, _ := m.Meta[MetaPlayedFraction].(float64)
	return playoutMeta{Finish: m.FinishReason, Cut: m.Meta[MetaPlayoutCut], HeardMs: heardMs, AudioMs: audioMs,
		Fraction: fraction}
}

// A reply the caller hears to the end reaches the transcript unchanged once
// its audio has played: the turn-end goes on at once, its Message held and
// stamped with the time it was held, and is released when the playout ends.
func TestPlayoutHold_FullyPlayedReplyReleasedUnchanged(t *testing.T) {
	r := startHold(t)
	got := r.pass(t, replyAudio(150*time.Millisecond), replyEnd("All done."))
	end := got[1]
	assert.True(t, end.Meta.PlayoutHeld, "the turn-end goes on at once, its Message held")
	assert.Equal(t, holdT0, end.Timestamp, "stamped when held")
	r.clock.advance(100 * time.Millisecond)
	assert.True(t, r.idle(t), "nothing released while the audio still plays")

	r.clock.advance(50 * time.Millisecond)
	rel := r.next(t)
	assert.True(t, rel.Meta.PlayoutRelease)
	assert.False(t, rel.EndOfStream, "the release is not a second turn end")
	assert.True(t, rel.Meta.PlayoutTurnEnd, "it records that the held element was one")
	assert.Equal(t, holdT0, rel.Timestamp, "the release keeps the held time")
	assert.Equal(t, types.Message{Role: roleAssistant, Content: "All done.", FinishReason: "stop",
		Meta: map[string]interface{}{finishReasonMetaKey: "stop"}}, *rel.Message)
	close(r.in)
	require.NoError(t, <-r.done)
}

// A reply cut by barge-in while it plays is released at once with its full
// text and finish reason, and what the caller heard of it.
func TestPlayoutHold_BargeInRecordsWhatWasHeard(t *testing.T) {
	r := startHold(t)
	got := r.pass(t, replyAudio(time.Second), replyEnd("A long story."))
	r.clock.advance(300 * time.Millisecond)
	b := r.pass(t, bargeInElem())[0]
	assert.True(t, b.Meta.Interrupted, "the barge-in itself goes on")
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, "A long story.", rel.Message.Content, "the text is not cut")
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: PlayoutCutBargeIn, HeardMs: 300, AudioMs: 1000, Fraction: 0.3},
		playoutOf(rel.Message))
	assert.Equal(t, "stop", rel.Message.Meta[finishReasonMetaKey])
	assert.Nil(t, got[1].Message.Meta[MetaHeardMs], "the caller's copy is untouched")
	close(r.in)
	require.NoError(t, <-r.done)
}

// A session that ends while a reply plays releases it with what was heard so
// far.
func TestPlayoutHold_InputCloseReleasesWhatWasHeard(t *testing.T) {
	r := startHold(t)
	r.pass(t, replyAudio(time.Second), replyEnd("A long story."))
	r.clock.advance(200 * time.Millisecond)
	close(r.in)
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: PlayoutCutSessionClosed, HeardMs: 200, AudioMs: 1000, Fraction: 0.2},
		playoutOf(rel.Message))
	require.NoError(t, <-r.done)
}

// Cancellation releases what is held (with what was heard) before the stage
// returns, and the stage still reads its input to the end.
func TestPlayoutHold_CancelReleasesAndDrains(t *testing.T) {
	r := startHold(t)
	r.pass(t, replyAudio(time.Second), replyEnd("A long story."))
	r.clock.advance(400 * time.Millisecond)
	r.cancel()
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease, "a held message is never lost")
	assert.Equal(t, int64(400), playoutOf(rel.Message).HeardMs)
	late := "after cancel"
	r.in <- StreamElement{Text: &late}
	assert.Equal(t, "after cancel", *r.next(t).Text, "input after cancellation still passes")
	close(r.in)
	assert.ErrorIs(t, <-r.done, context.Canceled)
}

// A reply without audio passes unchanged and is not held.
func TestPlayoutHold_TextReplyPassesUnchanged(t *testing.T) {
	r := startHold(t)
	end := r.pass(t, replyEnd("Text only."))[0]
	assert.False(t, end.Meta.PlayoutHeld)
	assert.True(t, end.Timestamp.IsZero())
	close(r.in)
	require.NoError(t, <-r.done)
	_, more := <-r.out
	assert.False(t, more, "nothing is released for a reply that was not held")
}

// Messages after a held reply are held behind it, so the transcript keeps
// its order; a barge-in releases them all, recording only the reply it cut.
func TestPlayoutHold_LaterMessagesKeepTheirPlace(t *testing.T) {
	r := startHold(t)
	r.pass(t, replyAudio(50*time.Millisecond), replyEnd("Short."))
	r.clock.advance(10 * time.Millisecond)
	r.pass(t, StreamElement{Message: &types.Message{Role: roleUser, Content: "and then?"}},
		replyAudio(time.Second), replyEnd("Long."))
	r.clock.advance(240 * time.Millisecond)
	r.pass(t, bargeInElem())
	var released []string
	var cut playoutMeta
	for range 3 {
		e := r.next(t)
		require.True(t, e.Meta.PlayoutRelease)
		released = append(released, e.Message.Role+":"+e.Message.Content)
		if e.Message.Meta[MetaPlayoutCut] != nil {
			cut = playoutOf(e.Message)
		}
	}
	assert.Equal(t, []string{"assistant:Short.", "user:and then?", "assistant:Long."}, released)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: PlayoutCutBargeIn, HeardMs: 200, AudioMs: 1000, Fraction: 0.2}, cut,
		"the second reply started playing when the first ended, 40 ms after it arrived")
	close(r.in)
	require.NoError(t, <-r.done)
}

// Audio of a reply that ended without a message (an error) is not credited
// to the next reply.
func TestPlayoutHold_ErroredReplyAudioNotCarriedOver(t *testing.T) {
	r := startHold(t)
	r.pass(t, replyAudio(500*time.Millisecond), StreamElement{Error: errors.New("provider dropped")})
	r.clock.advance(500 * time.Millisecond)
	r.pass(t, replyAudio(100*time.Millisecond), replyEnd("Second."))
	r.clock.advance(50 * time.Millisecond)
	r.pass(t, bargeInElem())
	rel := r.next(t)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: PlayoutCutBargeIn, HeardMs: 50, AudioMs: 100, Fraction: 0.5},
		playoutOf(rel.Message))
	close(r.in)
	require.NoError(t, <-r.done)
}

// The pipeline's completion totals count a held message once: the release
// repeats it.
func TestCompletionTotals_CountHeldMessageOnce(t *testing.T) {
	msg := &types.Message{Role: roleAssistant, Content: "x",
		CostInfo: &types.CostInfo{InputTokens: 3, OutputTokens: 4, TotalCost: 0.5}}
	held := StreamElement{Message: msg, EndOfStream: true}
	held.Meta.PlayoutHeld = true
	release := StreamElement{Message: msg}
	release.Meta.PlayoutRelease = true
	totals := &completionTotals{}
	assert.True(t, totals.observe(&held))
	assert.True(t, totals.observe(&release))
	assert.Equal(t, 1, totals.messageCount)
	assert.Equal(t, 3, totals.inputTokens)
	assert.Equal(t, 4, totals.outputTokens)
	assert.InDelta(t, 0.5, totals.totalCost, 1e-9)
}

// eosCounter counts the turn ends and the messages a stage downstream of the
// hold observes.
type eosCounter struct {
	BaseStage
	mu       sync.Mutex
	ends     int
	messages []string
}

func (c *eosCounter) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)
	for e := range input {
		c.mu.Lock()
		if e.EndOfStream {
			c.ends++
		}
		if e.Message != nil && !e.Meta.PlayoutHeld {
			c.messages = append(c.messages, e.Message.Content)
		}
		c.mu.Unlock()
		select {
		case output <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Each held reply ends its turn exactly once for every stage after the hold
// (recording, save, the session's turn boundary), whether it played to the
// end or was cut, and its message reaches the transcript once.
func TestPlayoutHold_OneTurnEndPerReply(t *testing.T) {
	fc := &fakeHoldClock{t: holdT0}
	hold := NewPlayoutHoldStage()
	hold.clock = holdClock{now: fc.now, after: fc.after}
	rec := NewRecordingStage(&fakeEventStore{}, RecordingStageConfig{Position: RecordingPositionOutput})
	counter := &eosCounter{BaseStage: NewBaseStage("eos-counter", StageTypeTransform)}
	p, err := NewPipelineBuilder().Chain(hold, rec, counter, NewPassthroughStage("sink")).Build()
	require.NoError(t, err)
	in := make(chan StreamElement, 16)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)
	next := func() StreamElement {
		select {
		case e := <-out:
			return e
		case <-time.After(3 * time.Second):
			require.FailNow(t, "no element came out")
			return StreamElement{}
		}
	}

	in <- replyAudio(100 * time.Millisecond) // played to the end
	in <- replyEnd("First.")
	next()
	next()
	fc.advance(100 * time.Millisecond)
	next()                        // released
	in <- replyAudio(time.Second) // cut
	in <- replyEnd("Second.")
	next()
	next()
	in <- bargeInElem()
	next()
	next() // released
	close(in)
	for range out { //nolint:revive // drain
	}

	counter.mu.Lock()
	defer counter.mu.Unlock()
	assert.Equal(t, 2, counter.ends, "one turn end per reply")
	assert.Equal(t, []string{"First.", "Second."}, counter.messages, "each reply reaches the transcript once")
}
