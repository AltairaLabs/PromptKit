package gemini

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A pipeline that holds a reply back until its guardrails judged it needs to
// know when the reply's content is complete: Gemini Live's turnComplete comes
// about a reply's playing time later, its generationComplete at once.

// To a pipeline that holds reply audio back (HoldPlayout), a reply's
// generationComplete is reported as a chunk marked GenerationComplete, in the
// reply's generation, after its content and before its turn completion.
func TestLiveReplyHold_GenerationCompleteIsReported(t *testing.T) {
	withContent := audioSecond()
	withContent.ServerContent.GenerationComplete = true
	for name, msgs := range map[string][]*ServerMessage{
		"on its own":       {said("Hello there."), audioSecond(), generationComplete(), turnComplete(textUsage(10, 5))},
		"with the content": {said("Hello there."), withContent, turnComplete(textUsage(10, 5))},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProtocolTestSession(t)
			s.HoldPlayout()
			process(t, s, msgs...)
			assertGenerationCompleteOrder(t, drainChunks(t, s))
		})
	}
}

func assertGenerationCompleteOrder(t *testing.T, chunks []providers.StreamChunk) {
	t.Helper()
	var kinds []string
	for _, c := range chunks {
		switch {
		case c.GenerationComplete:
			kinds = append(kinds, "generation_complete")
			assert.Equal(t, chunks[0].Generation, c.Generation, "the reply's generation")
			assert.Empty(t, c.Delta)
			assert.Nil(t, c.MediaData)
			assert.Nil(t, c.FinishReason)
		case c.FinishReason != nil:
			kinds = append(kinds, "turn_complete")
		case c.MediaData != nil:
			kinds = append(kinds, "audio")
		case c.Delta != "":
			kinds = append(kinds, "text")
		}
	}
	assert.Equal(t, []string{"text", "audio", "generation_complete", "turn_complete"}, kinds)
}

// A tool-call generation's generationComplete is not reported: no reply's
// content ends there, even when the model spoke before calling. Nor is one
// that arrives with its turn's completion.
func TestLiveReplyHold_GenerationCompleteNotReported(t *testing.T) {
	withTurn := audioSecond()
	withTurn.ServerContent.GenerationComplete = true
	withTurn.ServerContent.TurnComplete = true
	for name, msgs := range map[string][]*ServerMessage{
		"tool call":        {toolCall("c1", "Rome"), generationComplete(), turnComplete(textUsage(919, 12))},
		"spoken tool call": {said("Let me check."), toolCall("c1", "Rome"), generationComplete()},
		"with the turn":    {said("Hi."), withTurn},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProtocolTestSession(t)
			s.HoldPlayout()
			process(t, s, msgs...)
			chunks := drainChunks(t, s)
			require.NotEmpty(t, chunks)
			for _, c := range chunks {
				assert.False(t, c.GenerationComplete)
			}
		})
	}
}

// With its playout held, the session counts a reply's audio as playing only
// from its release: speech 3.5 s after two seconds of audio arrived, 0.5 s
// after its release, is barge-in, while audio the pipeline dropped never
// plays, even 1.5 s after it arrived. Audio still held, neither released nor
// dropped, waits for the caller: speaking over it is barge-in, and the
// session reports it so the pipeline releases none of it.
func TestLiveReplyHold_HeldPlayoutCountsFromRelease(t *testing.T) {
	for _, tc := range []struct {
		name     string
		release  bool
		drop     bool
		speakAt  time.Duration
		wantBarg bool
		wantHeld bool
	}{
		{"released", true, true, 3500 * time.Millisecond, true, false},
		{"released and played out", true, false, 6 * time.Second, false, false},
		{"dropped", false, true, 1500 * time.Millisecond, false, false},
		{"still held", false, false, 1500 * time.Millisecond, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, now := playoutSession(t)
			t0 := *now
			s.HoldPlayout()
			process(t, s, audioSecond(), audioSecond(), &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
			held := drainChunks(t, s)
			*now = t0.Add(3 * time.Second)
			if tc.release {
				for i := range held {
					s.ReleaseAudio(&held[i])
				}
			}
			if tc.drop {
				s.HeldAudioDropped(0)
			}
			*now = t0.Add(tc.speakAt)
			process(t, s, callerSays("wait"))

			assert.Equal(t, tc.wantBarg, barged(s))
			assert.Equal(t, tc.wantHeld, s.HeldAudioInterrupted(0))
			want := 0
			if tc.wantBarg {
				want = 1
			}
			assert.Equal(t, want, interrupted(drainChunks(t, s)))
		})
	}
}

// The session reports the most its transcript has been seen to trail its
// audio, so a pipeline releasing audio by transcript keeps that much back.
func TestLiveReplyHold_ReportsItsTranscriptLag(t *testing.T) {
	var s providers.StreamInputSession = newProtocolTestSession(t)
	lagger, ok := s.(providers.TranscriptLagger)
	require.True(t, ok)
	assert.Equal(t, 1500*time.Millisecond, lagger.OutputTranscriptLag())
}

// A pipeline that does not hold reply audio gets no GenerationComplete
// chunk: its Response() reads as it did before holds.
func TestLiveReplyHold_GenerationCompleteNotReportedWithoutAHold(t *testing.T) {
	s := newProtocolTestSession(t)
	process(t, s, said("Hello there."), audioSecond(), generationComplete(), turnComplete(textUsage(10, 5)))
	chunks := drainChunks(t, s)
	require.NotEmpty(t, chunks)
	for _, c := range chunks {
		assert.False(t, c.GenerationComplete)
	}
}

// Gemini reports barge-in itself only while it generates: once a held
// reply's generation completed, before its turn completes, the caller
// speaking over the held audio is a barge-in the session reports.
func TestLiveReplyHold_SpeechAfterGenerationCompleteOverHeldAudioBargesIn(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	s.HoldPlayout()
	process(t, s, audioSecond(), generationComplete())
	drainChunks(t, s)
	*now = t0.Add(2 * time.Second)
	process(t, s, callerSays("wait"))

	assert.True(t, barged(s))
	assert.True(t, s.HeldAudioInterrupted(0))
	assert.Equal(t, 1, interrupted(drainChunks(t, s)))
}

// Gemini's own interruption of a reply whose audio is still held ends the
// wait for it: the session reports the held audio interrupted, and the
// caller's speech after is not taken for a second barge-in on it.
func TestLiveReplyHold_ServerInterruptionEndsTheHeldWait(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	s.HoldPlayout()
	process(t, s, audioSecond(), generationComplete(),
		&ServerMessage{ServerContent: &ServerContent{Interrupted: true}},
		&ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
	assert.True(t, barged(s), "Gemini's interruption")
	assert.True(t, s.HeldAudioInterrupted(0))
	drainChunks(t, s)
	*now = t0.Add(3 * time.Second)
	process(t, s, callerSays("and another thing"))

	assert.False(t, barged(s), "no second barge-in")
	assert.Zero(t, interrupted(drainChunks(t, s)))
}

// While a held reply is still generating, Gemini reports barge-in itself: the
// session does not take the caller's speech for one.
func TestLiveReplyHold_SpeechWhileAHeldReplyGeneratesIsLeftToGemini(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	s.HoldPlayout()
	process(t, s, audioSecond())
	drainChunks(t, s)
	*now = t0.Add(2 * time.Second)
	process(t, s, callerSays("wait"))

	assert.False(t, barged(s))
	assert.False(t, s.HeldAudioInterrupted(0))
}

// Speech over a passed reply still playing, after its generation completed
// and before its turn completes, is a barge-in the session reports; Gemini's
// own interruption of the same reply, before or after, adds no second one.
func TestLiveReplyHold_SpeechOverPassedAudioPlayingBargesInOnce(t *testing.T) {
	interruptedMsg := &ServerMessage{ServerContent: &ServerContent{Interrupted: true}}
	for name, msgs := range map[string][]*ServerMessage{
		"the session alone": {callerSays("wait")},
		"then Gemini":       {callerSays("wait"), interruptedMsg},
		"Gemini first":      {interruptedMsg, callerSays("wait")},
	} {
		t.Run(name, func(t *testing.T) {
			s, now := playoutSession(t)
			t0 := *now
			s.HoldPlayout()
			process(t, s, audioSecond(), generationComplete())
			for _, c := range drainChunks(t, s) {
				s.ReleaseAudio(&c) // released at t0: plays until t0+1s
			}
			*now = t0.Add(500 * time.Millisecond)
			process(t, s, msgs...)
			assert.True(t, barged(s))
			assert.Equal(t, 1, interrupted(drainChunks(t, s)), "one barge-in")
		})
	}
}

// Without a hold, speech in the window between generationComplete and
// turnComplete is left to Gemini, as before holds.
func TestLiveReplyHold_SpeechAfterGenerationCompleteWithoutAHoldIsLeftToGemini(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	process(t, s, audioSecond(), generationComplete())
	drainChunks(t, s)
	*now = t0.Add(500 * time.Millisecond)
	process(t, s, callerSays("wait"))
	assert.False(t, barged(s))
}

// The reply's usage still goes when Gemini's interruption of a reply the
// session already reported interrupted is not reported again.
func TestLiveReplyHold_ASecondInterruptionStillCarriesTheUsage(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	s.HoldPlayout()
	process(t, s, audioSecond(), generationComplete())
	for _, c := range drainChunks(t, s) {
		s.ReleaseAudio(&c)
	}
	*now = t0.Add(500 * time.Millisecond)
	process(t, s, callerSays("wait"), &ServerMessage{
		ServerContent: &ServerContent{Interrupted: true}, UsageMetadata: textUsage(10, 5)})
	var costs int
	for _, c := range drainChunks(t, s) {
		if c.CostInfo != nil {
			costs++
			assert.Equal(t, 10, c.CostInfo.InputTokens)
		}
	}
	assert.Equal(t, 1, costs)
}

// Dropping an older reply's hold leaves a newer reply's held audio waiting
// for the caller, and its interruption standing.
func TestLiveReplyHold_DroppingAReplyLeavesANewerOnesHeldAudio(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	s.HoldPlayout()
	process(t, s, audioSecond(), &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
	first := drainChunks(t, s)[0].Generation
	process(t, s, audioSecond(), generationComplete())
	second := drainChunks(t, s)[0].Generation
	require.NotEqual(t, first, second)
	s.HeldAudioDropped(first)
	*now = t0.Add(2 * time.Second)
	process(t, s, callerSays("wait"))

	assert.True(t, barged(s), "the newer reply's audio still waited")
	s.HeldAudioDropped(first)
	assert.True(t, s.HeldAudioInterrupted(second))
	assert.False(t, s.ReleaseAudio(&providers.StreamChunk{Generation: second}))
}

// A reply after one the session reported interrupted has its own
// interruption by Gemini reported.
func TestLiveReplyHold_ALaterReplysInterruptionIsReported(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	s.HoldPlayout()
	process(t, s, audioSecond(), generationComplete())
	for _, c := range drainChunks(t, s) {
		s.ReleaseAudio(&c)
	}
	*now = t0.Add(500 * time.Millisecond)
	process(t, s, callerSays("wait"))
	assert.Equal(t, 1, interrupted(drainChunks(t, s)))
	process(t, s, audioSecond(), &ServerMessage{ServerContent: &ServerContent{Interrupted: true}})
	assert.Equal(t, 1, interrupted(drainChunks(t, s)), "the later reply's interruption")
}
