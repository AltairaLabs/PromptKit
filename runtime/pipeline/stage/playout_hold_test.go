package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

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

// holdRun drives a PlayoutHoldStage, recording each element with when it
// came out.
type holdRun struct {
	in    chan StreamElement
	out   chan StreamElement
	start time.Time
	done  chan error
}

func startHold() *holdRun {
	r := &holdRun{in: make(chan StreamElement, 16), out: make(chan StreamElement, 64), start: time.Now(),
		done: make(chan error, 1)}
	go func() { r.done <- NewPlayoutHoldStage().Process(context.Background(), r.in, r.out) }()
	return r
}

// next returns the next output element and how long after the start it came.
func (r *holdRun) next(t *testing.T) (StreamElement, time.Duration) {
	t.Helper()
	select {
	case e := <-r.out:
		return e, time.Since(r.start)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "no element came out")
		return StreamElement{}, 0
	}
}

// heard is the heard-playout metadata of a released reply.
func heard(m *types.Message) (finish string, cause any, heardMs, audioMs int64, fraction float64) {
	heardMs, _ = m.Meta[MetaHeardMs].(int64)
	audioMs, _ = m.Meta[MetaAudioMs].(int64)
	fraction, _ = m.Meta[MetaPlayedFraction].(float64)
	return m.FinishReason, m.Meta[types.MetaInterruptedCause], heardMs, audioMs, fraction
}

// A reply the caller hears to the end reaches the transcript unchanged once
// its audio has played: the turn-end goes on at once, its Message held, and
// is released when the playout ends.
func TestPlayoutHold_FullyPlayedReplyReleasedUnchanged(t *testing.T) {
	r := startHold()
	r.in <- replyAudio(150 * time.Millisecond)
	r.in <- replyEnd("All done.")

	_, _ = r.next(t) // the audio
	end, _ := r.next(t)
	assert.True(t, end.EndOfStream)
	assert.True(t, end.Meta.PlayoutHeld, "the turn-end goes on at once, its Message held")
	assert.Equal(t, "All done.", end.Message.Content)

	rel, at := r.next(t)
	assert.True(t, rel.Meta.PlayoutRelease)
	assert.GreaterOrEqual(t, at, 140*time.Millisecond, "released only after the audio played")
	assert.Equal(t, "All done.", rel.Message.Content)
	assert.Equal(t, "stop", rel.Message.FinishReason)
	assert.Equal(t, map[string]interface{}{finishReasonMetaKey: "stop"}, rel.Message.Meta)
	close(r.in)
	require.NoError(t, <-r.done)
}

// A reply cut by barge-in while it plays is released at once with its full
// text, finish reason interrupted, and what the caller heard of it.
func TestPlayoutHold_BargeInMarksWhatWasHeard(t *testing.T) {
	r := startHold()
	r.in <- replyAudio(time.Second)
	r.in <- replyEnd("A long story.")
	time.Sleep(300 * time.Millisecond)
	barge := StreamElement{}
	barge.Meta.Interrupted = true
	r.in <- barge

	_, _ = r.next(t)
	end, _ := r.next(t)
	require.True(t, end.Meta.PlayoutHeld)
	b, _ := r.next(t)
	assert.True(t, b.Meta.Interrupted, "the barge-in itself goes on")
	rel, _ := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, "A long story.", rel.Message.Content, "the text is not cut")
	finish, cause, heardMs, audioMs, fraction := heard(rel.Message)
	assert.Equal(t, types.FinishReasonInterrupted, finish)
	assert.Equal(t, InterruptedCausePlayout, cause)
	assert.Equal(t, finishReasonInterrupted, rel.Message.Meta[finishReasonMetaKey])
	assert.InDelta(t, 300, heardMs, 80)
	assert.Equal(t, int64(1000), audioMs)
	assert.InDelta(t, 0.3, fraction, 0.08)
	assert.Equal(t, "stop", end.Message.FinishReason, "the caller's copy is untouched")
	close(r.in)
	require.NoError(t, <-r.done)
}

// A session that ends while a reply plays releases it with what was heard so
// far.
func TestPlayoutHold_SessionCloseReleasesWhatWasHeard(t *testing.T) {
	r := startHold()
	r.in <- replyAudio(time.Second)
	r.in <- replyEnd("A long story.")
	time.Sleep(200 * time.Millisecond)
	close(r.in)
	require.NoError(t, <-r.done)

	var rel StreamElement
	for e := range r.out {
		if e.Meta.PlayoutRelease {
			rel = e
		}
	}
	require.NotNil(t, rel.Message)
	finish, cause, heardMs, audioMs, _ := heard(rel.Message)
	assert.Equal(t, types.FinishReasonInterrupted, finish)
	assert.Equal(t, InterruptedCauseSessionClosed, cause)
	assert.InDelta(t, 200, heardMs, 80)
	assert.Equal(t, int64(1000), audioMs)
}

// A reply without audio passes unchanged and is not held.
func TestPlayoutHold_TextReplyPassesUnchanged(t *testing.T) {
	r := startHold()
	r.in <- replyEnd("Text only.")
	end, _ := r.next(t)
	assert.False(t, end.Meta.PlayoutHeld)
	close(r.in)
	require.NoError(t, <-r.done)
	_, more := <-r.out
	assert.False(t, more, "nothing is released for a reply that was not held")
}

// Messages after a held reply are held behind it, so the transcript keeps
// its order; a barge-in releases them all, cutting only the reply it cut.
func TestPlayoutHold_LaterMessagesKeepTheirPlace(t *testing.T) {
	r := startHold()
	r.in <- replyAudio(50 * time.Millisecond)
	r.in <- replyEnd("Short.")
	r.in <- StreamElement{Message: &types.Message{Role: roleUser, Content: "and then?"}}
	r.in <- replyAudio(time.Second)
	r.in <- replyEnd("Long.")
	time.Sleep(250 * time.Millisecond)
	barge := StreamElement{}
	barge.Meta.Interrupted = true
	r.in <- barge
	close(r.in)
	require.NoError(t, <-r.done)

	var released []string
	for e := range r.out {
		if !e.Meta.PlayoutRelease {
			continue
		}
		_, cause, heardMs, _, _ := heard(e.Message)
		released = append(released, e.Message.Role+":"+e.Message.Content+":"+string(e.Message.FinishReason))
		if cause != nil {
			assert.InDelta(t, 200, heardMs, 80, "the second reply started after the first played")
		}
	}
	assert.Equal(t, []string{"assistant:Short.:stop", "user:and then?:", "assistant:Long.:interrupted"}, released)
}
