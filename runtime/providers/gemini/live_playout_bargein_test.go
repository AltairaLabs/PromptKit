package gemini

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Gemini Live sends serverContent.interrupted only while the model is
// generating. A reply's audio arrives faster than real time, so most of a
// long reply is still playing after turnComplete; the caller speaking then
// (first seen as the transcript of their speech) is barge-in too.

// audioSecond is one model turn carrying a second of 24 kHz PCM16 audio.
func audioSecond() *ServerMessage {
	pcm := base64.StdEncoding.EncodeToString(make([]byte, 2*defaultOutputSampleRate))
	return &ServerMessage{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{
		{InlineData: &InlineData{MimeType: "audio/pcm;rate=24000", Data: pcm}},
	}}}}
}

func callerSays(text string) *ServerMessage {
	return &ServerMessage{ServerContent: &ServerContent{InputTranscription: &Transcription{Text: text}}}
}

// playoutSession is a protocol test session on a clock set to t0.
func playoutSession(t *testing.T) (*StreamSession, *time.Time) {
	t.Helper()
	s := newProtocolTestSession(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func process(t *testing.T, s *StreamSession, msgs ...*ServerMessage) {
	t.Helper()
	for _, m := range msgs {
		require.NoError(t, s.processServerMessage(m))
	}
}

func interrupted(chunks []providers.StreamChunk) int {
	n := 0
	for _, c := range chunks {
		if c.Interrupted {
			n++
		}
	}
	return n
}

func barged(s *StreamSession) bool {
	select {
	case <-s.BargeIn():
		return true
	default:
		return false
	}
}

func TestLivePlayout_SpeechWhileFinishedReplyPlaysBargesIn(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	process(t, s, audioSecond(), audioSecond(), &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
	*now = t0.Add(1500 * time.Millisecond)
	process(t, s, callerSays("wait"), callerSays(" a moment"))

	chunks := drainChunks(t, s)
	assert.Equal(t, 1, interrupted(chunks), "the interruption is reported once")
	assert.True(t, barged(s), "BargeIn() fired")
	var transcripts []string
	for _, c := range chunks {
		if tr, ok := c.Metadata["transcription"].(string); ok {
			transcripts = append(transcripts, tr)
		}
	}
	assert.Equal(t, []string{"wait", " a moment"}, transcripts, "the caller's speech is still transcribed")

	// The reply to the caller's speech plays: the drop ended with the barge-in.
	process(t, s, audioSecond())
	var audio int
	for _, c := range drainChunks(t, s) {
		if c.MediaData != nil {
			audio++
		}
	}
	assert.Equal(t, 1, audio)
}

func TestLivePlayout_SpeechAfterReplyPlayedOutIsATurnStart(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	process(t, s, audioSecond(), audioSecond(), &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
	*now = t0.Add(2 * time.Second)
	process(t, s, callerSays("next question"))

	assert.Zero(t, interrupted(drainChunks(t, s)))
	assert.False(t, barged(s))
}

// A transcript that arrives while the model is still generating is the
// caller's earlier speech arriving late (the server reports barge-in during
// generation itself), not barge-in.
func TestLivePlayout_TranscriptDuringGenerationIsNotBargeIn(t *testing.T) {
	s, _ := playoutSession(t)
	process(t, s, audioSecond(), callerSays("what is the weather"), audioSecond())

	assert.Zero(t, interrupted(drainChunks(t, s)))
	assert.False(t, barged(s))
}

// Audio delivered after the earlier audio ran out plays from its arrival.
func TestLivePlayout_EstimateRestartsAfterSilence(t *testing.T) {
	s, now := playoutSession(t)
	t0 := *now
	process(t, s, audioSecond(), &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
	*now = t0.Add(10 * time.Second)
	process(t, s, audioSecond(), &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
	*now = t0.Add(10*time.Second + 900*time.Millisecond)
	process(t, s, callerSays("stop"))

	assert.Equal(t, 1, interrupted(drainChunks(t, s)))
}

// Once the model answers again (here its transcript comes first), a
// transcript of the caller is their earlier speech arriving late.
func TestLivePlayout_TranscriptAfterTheNextReplyStartsIsNotBargeIn(t *testing.T) {
	s, _ := playoutSession(t)
	process(t, s, audioSecond(), &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}},
		&ServerMessage{ServerContent: &ServerContent{OutputTranscription: &Transcription{Text: "Sure,"}}},
		callerSays("tell me more"))

	assert.Zero(t, interrupted(drainChunks(t, s)))
	assert.False(t, barged(s))
}

// Media without a sample rate (an image) adds nothing to the playout.
func TestLivePlayout_NonAudioMediaDoesNotPlay(t *testing.T) {
	s, _ := playoutSession(t)
	img := base64.StdEncoding.EncodeToString([]byte("not really a png"))
	process(t, s, &ServerMessage{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{
		{InlineData: &InlineData{MimeType: "image/png", Data: img}},
	}}, TurnComplete: true}}, callerSays("nice"))

	chunks := drainChunks(t, s)
	assert.Zero(t, interrupted(chunks))
	require.NotEmpty(t, chunks)
	assert.Equal(t, "image/png", chunks[0].MediaData.MIMEType)
}

// lateTranscriptSession plays the caller saying "what is the weather" (the
// end of it transcribed late, during generation), then a 5 s reply that is
// complete at t0+1s and still playing.
func lateTranscriptSession(t *testing.T, finished bool) (*StreamSession, func(time.Duration)) {
	t.Helper()
	s, now := playoutSession(t)
	t0 := *now
	at := func(d time.Duration) { *now = t0.Add(d) }
	first := callerSays("what is")
	first.ServerContent.InputTranscription.Finished = finished
	process(t, s, first)
	at(200 * time.Millisecond)
	process(t, s, audioSecond(), audioSecond(), audioSecond(), audioSecond(), audioSecond())
	if !finished {
		at(500 * time.Millisecond)
		process(t, s, callerSays(" the"))
	}
	at(time.Second)
	process(t, s, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}})
	return s, at
}

// The rest of the caller's earlier utterance, transcribed after the reply
// completed, is not barge-in.
func TestLivePlayout_LateTranscriptOfTheEarlierUtteranceIsNotBargeIn(t *testing.T) {
	s, at := lateTranscriptSession(t, false)
	at(1300 * time.Millisecond)
	process(t, s, callerSays(" weather"))

	assert.Zero(t, interrupted(drainChunks(t, s)))
	assert.False(t, barged(s))
}

// After a pause, the caller is saying something new: barge-in.
func TestLivePlayout_NewUtteranceAfterTheLateTranscriptBargesIn(t *testing.T) {
	s, at := lateTranscriptSession(t, false)
	at(1300 * time.Millisecond)
	process(t, s, callerSays(" weather"))
	at(3 * time.Second)
	process(t, s, callerSays("hold on"))

	assert.Equal(t, 1, interrupted(drainChunks(t, s)))
	assert.True(t, barged(s))
}

// An utterance Gemini marked finished is over: the next transcript is new.
func TestLivePlayout_TranscriptAfterAFinishedUtteranceBargesIn(t *testing.T) {
	s, at := lateTranscriptSession(t, true)
	at(1100 * time.Millisecond)
	process(t, s, callerSays("hold on"))

	assert.Equal(t, 1, interrupted(drainChunks(t, s)))
}
