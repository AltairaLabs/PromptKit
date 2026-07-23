package stage

import (
	"context"
	"sync/atomic"
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

// TestForwardResponseElements_ResetsIdleOnActivity is the death fix: a
// continuous voice session streams audio for many seconds, but the pipeline's
// 30s idle timeout cancels it unless a stage signals activity. The duplex stage
// must reset the idle timer as response chunks arrive — otherwise every live
// conversation dies at exactly 30s (context canceled / ErrIdleTimeout).
func TestForwardResponseElements_ResetsIdleOnActivity(t *testing.T) {
	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) })

	sess := newRecordingSession()
	s := stageWithSession(sess)
	out := make(chan StreamElement, 16)

	go func() {
		for range 5 {
			sess.respCh <- providers.StreamChunk{MediaData: &providers.StreamMediaData{
				Data: []byte{1, 2}, SampleRate: 24000, Channels: 1,
			}}
		}
		close(sess.respCh)
	}()

	require.NoError(t, s.forwardResponseElements(ctx, out))
	assert.Positive(t, resets.Load(),
		"the duplex stage must reset the pipeline idle timer on response activity, or live sessions die at the 30s idle timeout")
}

// TestForwardInputElements_ResetsIdleOnActivity: inbound microphone audio must
// also keep the pipeline alive.
func TestForwardInputElements_ResetsIdleOnActivity(t *testing.T) {
	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) })

	sess := newRecordingSession()
	s := stageWithSession(sess)
	input := make(chan StreamElement, 4)
	output := make(chan StreamElement, 4)
	done := make(chan error, 1)

	input <- duplexAudioElem(pcm(320))
	input <- duplexAudioElem(pcm(320))
	close(input)

	go s.forwardInputElements(ctx, input, output, done)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInputElements did not finish")
	}
	assert.Positive(t, resets.Load(),
		"the duplex stage must reset the pipeline idle timer on inbound audio activity")
}

// TestDuplexProviderStage_ActivityDefeatsIdleTimeout is the end-to-end proof of
// the death fix, against the REAL idle-timeout mechanism (not a stub): with a
// short idle timeout and a steady stream of response chunks arriving faster than
// the timeout, the session must survive well past the timeout. Without the reset
// the idle timer fires and cancels the context mid-stream (the ~30s death users
// hit); with it, each chunk resets the timer and the session lives.
func TestDuplexProviderStage_ActivityDefeatsIdleTimeout(t *testing.T) {
	const (
		idle     = 100 * time.Millisecond
		interval = 30 * time.Millisecond // < idle: activity keeps resetting the timer
		chunks   = 12                    // ~360ms total, 3.6x the idle timeout
	)

	idleCtx, cancel, reset := withIdleTimeout(context.Background(), idle)
	defer cancel()
	ctx := contextWithIdleReset(idleCtx, reset)

	sess := newRecordingSession()
	s := stageWithSession(sess)
	out := make(chan StreamElement, chunks*2)

	go func() {
		for range chunks {
			select {
			case sess.respCh <- providers.StreamChunk{MediaData: &providers.StreamMediaData{
				Data: []byte{1, 2}, SampleRate: 24000, Channels: 1,
			}}:
			case <-ctx.Done():
				return
			}
			time.Sleep(interval)
		}
		close(sess.respCh)
	}()

	err := s.forwardResponseElements(ctx, out)
	require.NoError(t, err,
		"a live session must survive past the idle timeout while streaming; it died mid-reply")
	require.NotErrorIs(t, context.Cause(ctx), ErrIdleTimeout,
		"the idle timeout fired despite continuous activity")
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
