package stage

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForwardResponseElements_LongAudioBurstAllForwarded reproduces the reported
// failure at its source: a long assistant reply is a burst of many audio chunks.
// Every one must be forwarded to the pipeline output (streamed to the speaker) —
// if the stage drops, stalls, or ends the turn early under the burst, fewer
// audio elements arrive.
func TestForwardResponseElements_LongAudioBurstAllForwarded(t *testing.T) {
	const burst = 300

	sess := newRecordingSession()
	s := stageWithSession(sess)
	out := make(chan StreamElement, burst*2)

	// Producer: burst audio chunks, then a turn-complete FinishReason, then close.
	go func() {
		for i := range burst {
			sess.respCh <- providers.StreamChunk{
				MediaData: &providers.StreamMediaData{
					Data: []byte{byte(i), byte(i >> 8)}, SampleRate: 24000, Channels: 1,
				},
			}
		}
		fr := "stop"
		sess.respCh <- providers.StreamChunk{FinishReason: &fr}
		close(sess.respCh)
	}()

	done := make(chan error, 1)
	go func() { done <- s.forwardResponseElements(context.Background(), out) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("forwardResponseElements did not finish — stalled under the burst")
	}

	var audioElems int
	for len(out) > 0 {
		e := <-out
		if e.Audio != nil && len(e.Audio.Samples) > 0 {
			audioElems++
		}
	}
	assert.Equal(t, burst, audioElems,
		"every audio chunk of the reply must be forwarded; got %d of %d", audioElems, burst)
}

// TestForwardInputElements_DrainSignalEndsInputPromptly covers the drain fix: a
// graceful Close sends an EndOfStream element marked AllResponsesReceived. The
// input loop must end input and signal an immediate session close (so the
// response loop skips its 30s final-response wait), rather than treating it as an
// ordinary end-of-turn.
func TestForwardInputElements_DrainSignalEndsInputPromptly(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)
	done := make(chan error, 1)

	input <- StreamElement{EndOfStream: true, Meta: ElementMetadata{AllResponsesReceived: true}}

	go s.forwardInputElements(context.Background(), input, output, done)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInputElements did not end on the drain signal")
	}

	// Both the input-done and skip-final-timeout channels must be closed, so the
	// response loop closes the session immediately instead of waiting.
	select {
	case <-s.inputDoneCh:
	default:
		t.Error("inputDoneCh not closed on drain signal")
	}
	select {
	case <-s.allResponsesReceivedCh:
	default:
		t.Error("allResponsesReceivedCh not closed on drain signal")
	}
}
