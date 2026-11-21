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

// A reply's generationComplete is reported as a chunk marked
// GenerationComplete, in the reply's generation, after its content and before
// its turn completion.
func TestLiveReplyHold_GenerationCompleteIsReported(t *testing.T) {
	withContent := audioSecond()
	withContent.ServerContent.GenerationComplete = true
	for name, msgs := range map[string][]*ServerMessage{
		"on its own":       {said("Hello there."), audioSecond(), generationComplete(), turnComplete(textUsage(10, 5))},
		"with the content": {said("Hello there."), withContent, turnComplete(textUsage(10, 5))},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProtocolTestSession(t)
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
					s.AudioReleased(&held[i])
				}
			}
			if tc.drop {
				s.HeldAudioDropped()
			}
			*now = t0.Add(tc.speakAt)
			process(t, s, callerSays("wait"))

			assert.Equal(t, tc.wantBarg, barged(s))
			assert.Equal(t, tc.wantHeld, s.HeldAudioInterrupted())
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
