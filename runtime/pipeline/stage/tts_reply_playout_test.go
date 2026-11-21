package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
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
	return startReplyWith(t, func(*TTSStageWithInterruptionConfig) {})
}

func startReplyWith(t *testing.T, configure func(*TTSStageWithInterruptionConfig)) *pacedReply {
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
	configure(&cfg)
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

// A barge-in on a round that spoke and called a tool keeps the round's tool
// calls (their results are already in the conversation) and its own finish
// reason, and truncates only its text to what was heard: the call and its
// result stay a pair in the history the model is sent, and in the transcript.
func TestTTSStageWithInterruption_BargeInOnAToolRoundKeepsItsCalls(t *testing.T) {
	p := startPacedReply(t)
	round := &types.Message{Role: roleAssistant, Content: "Let me look that up for you.",
		FinishReason: "tool_calls", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "get_weather"}}}
	p.send(t, textDelta("Let me look that up for you. "), NewMessageElement(round))
	p.audioUntil(t, 2)
	p.clock.advance(100 * time.Millisecond) // half the sentence has played

	p.send(t, NewInterruptElement())
	msg := p.messageNext(t)

	assert.Equal(t, "Let me look", msg.Content, "only the heard text")
	assert.Equal(t, round.ToolCalls, msg.ToolCalls, "the round's calls are kept")
	assert.Equal(t, "tool_calls", msg.FinishReason, "not interrupted: it stays in model context with its calls")
	assert.Equal(t, types.PlayoutCutBargeIn, msg.Meta[types.MetaPlayoutCut])
	assert.False(t, msg.LeavesModelContext())

	p.heard.expect()
	within(t, "await", func() bool { p.heard.await(context.Background()); return true })
	history := []types.Message{{Role: roleUser, Content: "weather?"}, *round,
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "get_weather"}}}
	got := ExcludeFromModelContext(p.heard.apply(history))
	require.Len(t, got, 3, "the call and its result go back to the model together")
	assert.Equal(t, round.ToolCalls, got[1].ToolCalls)
	assert.Equal(t, "Let me look", got[1].Content)
}

// The rounds of a tool-calling turn are spoken apart: a round's text ends a
// segment when the next round's begins (a delta of another message), though no
// space follows its last sentence. A barge-in during the first round records
// what was heard on that round's message, and none of the second on its own.
func TestTTSStageWithInterruption_RoundsAreSpokenAndCutApart(t *testing.T) {
	p := startPacedReply(t)
	round1 := textDelta("Let me look that up.")
	round1.Meta.MessageID = "m1"
	round2 := textDelta("It is sunny in Paris.")
	round2.Meta.MessageID = "m2"
	call := &types.Message{Role: roleAssistant, Content: "Let me look that up.", FinishReason: "tool_calls",
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "get_weather"}},
		Meta:      map[string]interface{}{types.MetaMessageID: "m1"}}
	answer := &types.Message{Role: roleAssistant, Content: "It is sunny in Paris.", FinishReason: "stop",
		Meta: map[string]interface{}{types.MetaMessageID: "m2"}}
	p.send(t, round1, round2)
	p.audioUntil(t, 2)
	assert.Equal(t, []string{"Let me look that up."}, p.texts(), "the first round is a segment of its own")
	p.clock.advance(100 * time.Millisecond) // half the first round's sentence has played
	p.send(t, NewMessageElement(call), NewMessageElement(&types.Message{Role: "tool",
		ToolResult: &types.MessageToolResult{ID: "c1", Name: "get_weather"}}), NewMessageElement(answer),
		NewEndOfTurnElement(), NewInterruptElement())

	first := p.messageNext(t)
	assert.Equal(t, "Let me", first.Content, "the first round as heard: half its five words")
	assert.Equal(t, call.ToolCalls, first.ToolCalls)
	p.messageNext(t) // the tool result
	second := p.messageNext(t)
	assert.Equal(t, "", second.Content, "none of the second round was heard")
	assert.Equal(t, types.FinishReasonInterrupted, second.FinishReason)
	assert.False(t, second.IsHeardPrefix())
}

// A barge-in during the second round keeps the first round whole (it was
// heard in full) and records the share heard of the second on its own message.
func TestTTSStageWithInterruption_BargeInOnTheSecondRound(t *testing.T) {
	p := startPacedReply(t)
	round1 := textDelta("Let me look that up.")
	round1.Meta.MessageID = "m1"
	round2 := textDelta("It is sunny in Paris.")
	round2.Meta.MessageID = "m2"
	stop := "stop"
	round2.Meta.FinishReason = &stop // the turn's last chunk
	call := &types.Message{Role: roleAssistant, Content: "Let me look that up.", FinishReason: "tool_calls",
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "get_weather"}},
		Meta:      map[string]interface{}{types.MetaMessageID: "m1"}}
	answer := &types.Message{Role: roleAssistant, Content: "It is sunny in Paris.", FinishReason: "stop",
		Meta: map[string]interface{}{types.MetaMessageID: "m2"}}
	p.send(t, round1, round2, NewMessageElement(call), NewMessageElement(answer), NewEndOfTurnElement())
	p.audioUntil(t, 4)
	p.clock.advance(200 * time.Millisecond) // the first round has played
	first := p.messageNext(t)
	assert.Equal(t, "Let me look that up.", first.Content, "heard in full")
	assert.NotContains(t, first.Meta, types.MetaPlayoutCut, "it goes on once its own audio has played")
	p.clock.advance(100 * time.Millisecond) // half the second
	p.send(t, NewInterruptElement())

	second := p.messageNext(t)
	assert.Equal(t, "It is", second.Content, "half the second round's five words")
	assert.Equal(t, int64(100), second.Meta[types.MetaHeardMs], "only its own audio counts")
	assert.Equal(t, int64(200), second.Meta[types.MetaAudioMs])
}

// A reply the caller heard all of before a barge-in cut its turn (the provider
// stage kept it as interrupted) is a heard prefix too: what was heard is the
// whole of it, so it goes back to the model.
func TestTTSStageWithInterruption_FullyHeardCutReplyIsAHeardPrefix(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("One two three four. "))
	p.audioUntil(t, 2)
	p.clock.advance(300 * time.Millisecond) // it has played in full
	kept := assistantMessage("One two three four. ")
	kept.Message.FinishReason = types.FinishReasonInterrupted
	p.send(t, kept)
	msg := p.messageNext(t)

	assert.Equal(t, "One two three four. ", msg.Content, "heard in full: its own text")
	assert.True(t, msg.IsHeardPrefix(), "what was heard goes back to the model")
}

// roundDelta is a streaming delta of round id's reply.
func roundDelta(id, text string, replaces bool) StreamElement {
	e := textDelta(text)
	e.Meta.MessageID = id
	e.Meta.ReplacesText = replaces
	return e
}

// roundMessage is round id's completed reply.
func roundMessage(id, text string, calls ...string) StreamElement {
	m := &types.Message{Role: roleAssistant, Content: text, Meta: map[string]interface{}{types.MetaMessageID: id}}
	for _, c := range calls {
		m.ToolCalls = append(m.ToolCalls, types.MessageToolCall{ID: c, Name: "get_weather"})
	}
	return NewMessageElement(m)
}

// A guardrail's replacement in the second round of a tool turn is compared
// with that round's spoken text, not the first round's: a rewrite that keeps
// what was spoken speaks only the rest, and the first round is neither spoken
// again nor stored twice.
func TestTTSStageWithInterruption_ASecondRoundReplacementIsThatRounds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		replacement string
		spoken      []string
		stored      string
	}{
		{"overlapping", "It is sunny. Call [REDACTED].",
			[]string{"Let me look that up.", "It is sunny.", "Call [REDACTED]."}, "It is sunny. Call [REDACTED]."},
		{"not overlapping", "Blocked.",
			[]string{"Let me look that up.", "It is sunny.", "Blocked."}, "It is sunny. Blocked."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := startPacedReply(t)
			p.send(t, roundDelta("m1", "Let me look that up. ", false), roundDelta("m2", "It is sunny. Call 555", false))
			p.audioUntil(t, 4)
			p.clock.advance(400 * time.Millisecond) // both sentences have played
			p.send(t, roundDelta("m2", tc.replacement, true))
			p.audioUntil(t, 6)
			p.clock.advance(time.Second)
			p.send(t, roundMessage("m1", "Let me look that up.", "c1"), roundMessage("m2", tc.replacement))
			first := p.messageNext(t)
			second := p.messageNext(t)

			assert.Equal(t, tc.spoken, p.texts(), "nothing spoken twice")
			assert.Equal(t, "Let me look that up.", first.Content, "the first round is stored once")
			assert.Equal(t, tc.stored, second.Content)
		})
	}
}

// Without playback pacing there is no playback to wait out: once the reply's
// turn ends the bot is no longer speaking, so caller speech after it is a new
// turn, not a barge-in on audio the caller is not playing.
func TestTTSStageWithInterruption_NoPlaybackPacingLeavesNoPlaybackWindow(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()
	p := startReplyWith(t, func(c *TTSStageWithInterruptionConfig) {
		c.NoPlaybackPacing = true
		c.InterruptionHandler = h
	})
	p.send(t, textDelta("One sentence here. "), assistantMessage("One sentence here."), NewEndOfTurnElement())
	for !p.next(t).EndOfTurn {
	}
	assert.False(t, h.IsBotSpeaking(), "the turn is over and no audio is counted as playing")
}

// A reply cut to what was heard, with no tool calls, is marked a heard
// prefix: it stays in model context though it is interrupted.
func TestTTSStageWithInterruption_BargeInMarksAHeardPrefix(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, textDelta("One two three four. "), assistantMessage("One two three four."))
	p.audioUntil(t, 2)
	p.clock.advance(100 * time.Millisecond)
	p.send(t, NewInterruptElement())
	msg := p.messageNext(t)

	assert.Equal(t, "One two", msg.Content)
	assert.True(t, msg.IsHeardPrefix())
}

// With NoPlaybackPacing (a client that does not play the audio in real time
// as it arrives) nothing waits on the playout: every segment is synthesized
// at once and the Message goes on at once, whole.
func TestTTSStageWithInterruption_NoPlaybackPacing(t *testing.T) {
	p := startReplyWith(t, func(c *TTSStageWithInterruptionConfig) { c.NoPlaybackPacing = true })
	p.send(t, textDelta("One sentence here. "), textDelta("Two sentence here. "), textDelta("Three sentence here. "),
		assistantMessage("One sentence here. Two sentence here. Three sentence here."))
	msg := p.messageNext(t) // the clock never moves
	assert.Equal(t, []string{"One sentence here.", "Two sentence here.", "Three sentence here."}, p.texts())
	assert.Equal(t, "One sentence here. Two sentence here. Three sentence here.", msg.Content)
	assert.NotContains(t, msg.Meta, types.MetaPlayoutCut)

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

// A reply that did not stream, spoken whole, opens no playback window either
// without playback pacing.
func TestTTSStageWithInterruption_NoPlaybackPacingWholeTextOpensNoWindow(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	p := startReplyWith(t, func(c *TTSStageWithInterruptionConfig) {
		c.NoPlaybackPacing = true
		c.InterruptionHandler = h
	})
	p.send(t, NewTextElement("A whole reply."))
	for p.next(t).Audio == nil {
	}
	assert.False(t, h.IsBotSpeaking(), "no audio is counted as playing")
}

// A round's Message does not speak the next round's unfinished text: what the
// second round has streamed so far waits for its own end.
func TestTTSStageWithInterruption_ARoundsMessageLeavesTheNextRoundsText(t *testing.T) {
	p := startPacedReply(t)
	p.send(t, roundDelta("m1", "Let me look that up. ", false), roundDelta("m2", "It is sun", false))
	p.audioUntil(t, 2)
	p.clock.advance(200 * time.Millisecond)
	p.send(t, roundMessage("m1", "Let me look that up.", "c1"))
	first := p.messageNext(t)

	assert.Equal(t, "Let me look that up.", first.Content)
	assert.Equal(t, []string{"Let me look that up."}, p.texts(), "the second round's fragment is not spoken as the first's")
}
