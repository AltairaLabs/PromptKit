package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// pcm24kFor is d of silence as 24 kHz 16-bit PCM, fakeTTS's rate.
func pcm24kFor(d time.Duration) []byte {
	return make([]byte, int(d*24000/time.Second)*2)
}

// pacedReply is a TTSStageWithInterruption on a manual clock, every 200ms of
// a segment's audio synthesized at once, run on its own goroutine.
type pacedReply struct {
	cancel context.CancelFunc
	heard  *HeardReplies
	svc    *fakeTTS
	clock  *fakeHoldClock
	input  chan StreamElement
	output chan StreamElement
	done   chan error
	got    []StreamElement
}

func startPacedReply(t *testing.T) *pacedReply {
	t.Helper()
	p := &pacedReply{
		svc:    newFakeTTS(pcm24kFor(200 * time.Millisecond)),
		clock:  &fakeHoldClock{t: holdT0},
		input:  make(chan StreamElement),
		output: make(chan StreamElement, 64),
		done:   make(chan error, 1),
		heard:  NewHeardReplies(),
	}
	cfg := DefaultTTSStageWithInterruptionConfig()
	cfg.Retry = tts.RetryConfig{MaxAttempts: 1}
	cfg.HeardReplies = p.heard
	s := NewTTSStageWithInterruption(p.svc, cfg)
	s.clock = holdClock{now: p.clock.now, until: p.clock.until}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.cancel = cancel
	go func() { p.done <- s.Process(ctx, p.input, p.output) }()
	return p
}

func (p *pacedReply) send(t *testing.T, elems ...StreamElement) {
	t.Helper()
	for _, e := range elems {
		within(t, "a send", func() bool { p.input <- e; return true })
	}
}

// next is the stage's next output element, failing the test when there is
// none by holdTestDeadline or the stage closed its output.
func (p *pacedReply) next(t *testing.T) StreamElement {
	t.Helper()
	select {
	case e, ok := <-p.output:
		if !ok {
			t.Fatal("the stage closed its output early")
		}
		p.got = append(p.got, e)
		return e
	case <-time.After(holdTestDeadline):
		t.Fatalf("the stage emitted nothing more after %d elements", len(p.got))
		return StreamElement{}
	}
}

// audioUntil reads on until n of the stage's elements carry audio.
func (p *pacedReply) audioUntil(t *testing.T, n int) {
	t.Helper()
	for countAudio(p.got) < n {
		p.next(t)
	}
}

// messageNext reads on to the next element with a Message.
func (p *pacedReply) messageNext(t *testing.T) *types.Message {
	t.Helper()
	for {
		if e := p.next(t); e.Message != nil {
			return e.Message
		}
	}
}

// quiet reports whether the stage emits no audio or Message for a moment
// (the deltas it forwards as they arrive are kept and skipped).
func (p *pacedReply) quiet() bool {
	deadline := time.After(30 * time.Millisecond)
	for {
		select {
		case e, ok := <-p.output:
			if !ok {
				return false
			}
			p.got = append(p.got, e)
			if e.Audio != nil || e.Message != nil {
				return false
			}
		case <-deadline:
			return true
		}
	}
}

func (p *pacedReply) texts() []string {
	p.svc.mu.Lock()
	defer p.svc.mu.Unlock()
	return append([]string(nil), p.svc.texts...)
}

func countAudio(elems []StreamElement) int {
	n := 0
	for _, e := range elems {
		if e.Audio != nil {
			n++
		}
	}
	return n
}

func textDelta(text string) StreamElement {
	e := NewTextElement(text)
	e.Meta.StreamingDelta = true
	return e
}

func assistantMessage(text string) StreamElement {
	return NewMessageElement(&types.Message{Role: roleAssistant, Content: text})
}

// At most one segment is synthesized ahead of the one playing: the third
// waits until the second starts to play, so a barge-in finds little audio
// spoken that was never heard.
func TestTTSStageWithInterruption_SynthesizesOneSegmentAhead(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("One sentence here. "), textDelta("Two sentence here. "), textDelta("Three sentence here. "))
	p.audioUntil(t, 4) // two 100ms pieces of each of the first two segments

	require.True(t, p.quiet(), "nothing more while the first segment plays")
	assert.Equal(t, []string{"One sentence here.", "Two sentence here."}, p.texts())

	p.clock.advance(200 * time.Millisecond) // the first segment has played
	p.audioUntil(t, 6)
	assert.Equal(t, []string{"One sentence here.", "Two sentence here.", "Three sentence here."}, p.texts())
}

// A reply's Message goes on once its audio has played, so it is stored as
// heard; until then the stage holds it.
func TestTTSStageWithInterruption_MessageWaitsForItsAudioToPlay(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("One sentence here. "), assistantMessage("One sentence here."))
	p.audioUntil(t, 2)
	require.True(t, p.quiet(), "the Message waits while its audio plays")

	p.clock.advance(200 * time.Millisecond)
	msg := p.messageNext(t)
	assert.Equal(t, "One sentence here.", msg.Content)
	assert.Empty(t, msg.FinishReason)
	assert.NotContains(t, msg.Meta, types.MetaPlayoutCut, "a reply heard in full is not marked cut")

	end := NewEndOfTurnElement()
	stop := "stop"
	end.Meta.FinishReason = &stop
	p.send(t, end)
	got := p.next(t)
	require.True(t, got.EndOfTurn)
	require.NotNil(t, got.Meta.FinishReason)
	assert.Equal(t, "stop", *got.Meta.FinishReason, "a reply heard in full ends as it ended")
}

// Once the input ends (the session is closing), a reply waiting for its
// audio to play stops waiting and is stored as heard by then, marked as cut
// by the session closing, with its own finish reason, as the realtime
// playout hold records it.
func TestTTSStageWithInterruption_InputEndStoresWhatWasHeard(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("One two three four. "), assistantMessage("One two three four."))
	p.audioUntil(t, 2)
	p.clock.advance(100 * time.Millisecond) // half the segment has played
	close(p.input)

	msg := p.messageNext(t)
	assert.Equal(t, "One two", msg.Content)
	assert.Empty(t, msg.FinishReason, "the reply keeps its own finish reason")
	assert.Equal(t, types.PlayoutCutSessionClosed, msg.Meta[types.MetaPlayoutCut])
	assert.Equal(t, int64(100), msg.Meta[types.MetaHeardMs])
	assert.Equal(t, int64(200), msg.Meta[types.MetaAudioMs])
	assert.NoError(t, recvWithin(t, p.done))
}

// A session canceled while a reply plays (Close) still hands on the reply,
// as heard by then, and every element queued behind it, before Process
// returns: nothing in flight is lost, and nothing more is synthesized.
func TestTTSStageWithInterruption_CancelHandsOnTheReplyAsHeard(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("One two three four. "), textDelta("Five six seven eight. "),
		assistantMessage("One two three four. Five six seven eight."), NewEndOfTurnElement())
	p.audioUntil(t, 4)
	p.clock.advance(100 * time.Millisecond) // half the first segment has played
	p.cancel()

	msg := p.messageNext(t)
	assert.Equal(t, "One two", msg.Content)
	assert.Equal(t, types.PlayoutCutSessionClosed, msg.Meta[types.MetaPlayoutCut])
	assert.True(t, p.next(t).EndOfTurn, "the turn end queued behind it is handed on too")
	close(p.input)
	assert.ErrorIs(t, recvWithin(t, p.done), context.Canceled)
	assert.Equal(t, []string{"One two three four.", "Five six seven eight."}, p.texts(),
		"nothing more is synthesized")
}

// A barge-in on a guardrail's message, spoken after the sentences it
// replaced the rest of, stores those sentences once, then what was heard of
// the message.
func TestTTSStageWithInterruption_BargeInAfterAReplacementStoresEachSentenceOnce(t *testing.T) {
	p := startPacedReply(t)
	replace := textDelta("Not allowed here.")
	replace.Meta.ReplacesText = true
	p.send(t, textDelta("Fine. Then a dra"))
	p.audioUntil(t, 2)
	p.send(t, replace, assistantMessage("Not allowed here."), NewEndOfTurnElement())
	p.audioUntil(t, 4)
	p.clock.advance(300 * time.Millisecond) // all of "Fine.", half the message

	p.send(t, NewInterruptElement())
	msg := p.messageNext(t)
	assert.Equal(t, "Fine. Not", msg.Content)
}

// A barge-in stores the reply as the caller heard it: the segments that had
// played, and of the one playing, the share of its words its audio had
// played. heard_ms, audio_ms, played_fraction and playout_cut record it as
// the realtime playout hold does.
func TestTTSStageWithInterruption_BargeInStoresWhatWasHeard(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("The fox woke early. "), textDelta("It ran to the river. "), textDelta("Then it slept. "),
		assistantMessage("The fox woke early. It ran to the river. Then it slept."), NewEndOfTurnElement())
	p.audioUntil(t, 4)
	p.clock.advance(200 * time.Millisecond) // the first segment has played; the third is synthesized
	p.audioUntil(t, 6)
	p.clock.advance(100 * time.Millisecond) // half the second has played

	p.send(t, NewInterruptElement())
	msg := p.messageNext(t)

	assert.Equal(t, "The fox woke early. It ran", msg.Content,
		"the first sentence, then the share of the second the caller heard")
	assert.Equal(t, types.FinishReasonInterrupted, msg.FinishReason)
	assert.Equal(t, types.PlayoutCutBargeIn, msg.Meta[types.MetaPlayoutCut])
	assert.Equal(t, int64(300), msg.Meta[types.MetaHeardMs])
	assert.Equal(t, int64(600), msg.Meta[types.MetaAudioMs])
	assert.InDelta(t, 0.5, msg.Meta[types.MetaPlayedFraction], 1e-9)

	end := p.next(t)
	require.True(t, end.EndOfTurn)
	require.NotNil(t, end.Meta.FinishReason)
	assert.Equal(t, types.FinishReasonInterrupted, *end.Meta.FinishReason, "the cut reply's turn ends interrupted")
	assert.True(t, p.next(t).Interrupt)

	// The provider stage's history gets the reply as heard, once the stage
	// has handled the Interrupt.
	old := heardSettleTimeout
	heardSettleTimeout = time.Minute // only the stage's settle ends the wait
	t.Cleanup(func() { heardSettleTimeout = old })
	p.heard.expect() // the provider stage passed the Interrupt on
	within(t, "await", func() bool { p.heard.await(context.Background()); return true })
	history := []types.Message{{Role: roleAssistant, Content: "The fox woke early. It ran to the river. Then it slept."}}
	got := p.heard.apply(history)
	assert.Equal(t, "The fox woke early. It ran", got[0].Content)
	assert.Equal(t, types.FinishReasonInterrupted, got[0].FinishReason)
}

// Each segment is synthesized with the text spoken before it and, when it is
// already known, the text after it, so a service that uses them (ElevenLabs)
// keeps the prosody continuous across segments.
func TestTTSStageWithInterruption_SegmentsCarryTheirNeighboursText(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("One sentence here. Two sentence here. Thr"))
	p.audioUntil(t, 4)

	p.svc.mu.Lock()
	configs := append([]tts.SynthesisConfig(nil), p.svc.configs...)
	p.svc.mu.Unlock()
	require.Len(t, configs, 2)
	assert.Equal(t, "", configs[0].PreviousText)
	assert.Equal(t, "Two sentence here.", configs[0].NextText)
	assert.Equal(t, "One sentence here.", configs[1].PreviousText)
	assert.Equal(t, "", configs[1].NextText, "the next segment is not complete yet")
}
