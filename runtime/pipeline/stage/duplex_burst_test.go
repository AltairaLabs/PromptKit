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
		for i := 0; i < burst; i++ {
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
