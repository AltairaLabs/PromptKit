package stage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// openTurnRig drives a duplex stage's response loop one chunk at a time:
// send pushes a chunk and returns once the stage has handled it.
type openTurnRig struct {
	t       *testing.T
	sess    *recordingSession
	handled chan struct{}
	done    chan error
}

func startOpenTurnRig(t *testing.T, ctx context.Context, s *DuplexProviderStage, sess *recordingSession) *openTurnRig {
	t.Helper()
	r := &openTurnRig{t: t, sess: sess, handled: make(chan struct{}, 16), done: make(chan error, 1)}
	s.chunkHandled = func() { r.handled <- struct{}{} }
	out := make(chan StreamElement, 64)
	go func() {
		r.done <- s.forwardResponseElements(ctx, out)
		close(out)
	}()
	go func() {
		for range out { // drain what the stage emits
		}
	}()
	return r
}

func (r *openTurnRig) send(c providers.StreamChunk) {
	r.t.Helper()
	r.sess.respCh <- c
	select {
	case <-r.handled:
	case <-time.After(idleProbeBound):
		r.t.Fatal("the stage never handled the chunk")
	}
}

func (r *openTurnRig) finish() {
	r.t.Helper()
	close(r.sess.respCh)
	select {
	case err := <-r.done:
		require.NoError(r.t, err)
	case <-time.After(idleProbeBound):
		r.t.Fatal("the response loop did not end")
	}
}

func audioChunk() providers.StreamChunk {
	return providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: []byte{1, 2}, SampleRate: 24000, Channels: 1,
	}}
}

func turnCompleteChunk() providers.StreamChunk {
	fr := "stop"
	return providers.StreamChunk{FinishReason: &fr}
}

// TestDuplexProviderStage_OpenTurnIsNotIdle is the regression for #2773:
// Gemini Live streams a long reply's audio in its first seconds but reports
// the turn complete only once the audio would have finished playing. Between
// the last chunk and turnComplete nothing arrives, yet the turn is in
// progress: the idle timer must be held open until the turn completes.
func TestDuplexProviderStage_OpenTurnIsNotIdle(t *testing.T) {
	probe := newIdleProbe()
	sess := newRecordingSession()
	s := stageWithSession(sess)
	r := startOpenTurnRig(t, probe.context(context.Background()), s, sess)

	r.send(audioChunk()) // the reply's last chunk; turnComplete comes much later
	for i := range 3 {
		probe.openWindow()
		require.True(t, probe.closeWindow(),
			"idle window %d lapsed while the provider's turn was still open", i)
	}

	r.send(turnCompleteChunk())
	r.finish()
}

// TestDuplexProviderStage_OpenTurnHoldEndsAtTurnComplete pins the other half:
// once the provider completes the turn the session is idle again, so the
// hold stops and the idle timer fires as normal.
func TestDuplexProviderStage_OpenTurnHoldEndsAtTurnComplete(t *testing.T) {
	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) }, 40*time.Millisecond)
	ticker := newFakeTicker()
	sess := newRecordingSession()
	s := stageWithSession(sess)
	s.idleHoldTicker = ticker.start
	r := startOpenTurnRig(t, ctx, s, sess)

	r.send(audioChunk())
	r.send(audioChunk()) // a second chunk of the same turn starts no second hold
	before := resets.Load()
	ticker.tick(t)
	require.Eventually(t, func() bool { return resets.Load() == before+1 }, idleProbeBound, time.Millisecond,
		"each heartbeat of an open turn resets the idle timer")
	r.send(turnCompleteChunk())

	select {
	case <-ticker.stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the open-turn hold kept running after the turn completed")
	}
	assert.Equal(t, 20*time.Millisecond, ticker.interval, "the hold ticks every half idle window")
	select {
	case ticker.ticks <- time.Time{}:
		t.Fatal("the hold took a tick after the turn completed")
	default:
	}
	r.finish()
}

// TestDuplexProviderStage_OpenTurnHoldIsBounded: a provider that never
// completes its turn is a stuck session, not a long reply. The hold gives up
// after openTurnMaxHold so the idle timer can still end it.
func TestDuplexProviderStage_OpenTurnHoldIsBounded(t *testing.T) {
	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) }, 40*time.Millisecond)
	ticker := newFakeTicker()
	sess := newRecordingSession()
	s := stageWithSession(sess)
	s.idleHoldTicker = ticker.start
	s.openTurnMaxHold = 60 * time.Millisecond // three 20ms heartbeats
	r := startOpenTurnRig(t, ctx, s, sess)

	r.send(audioChunk())
	afterChunk := resets.Load()
	for range 3 {
		ticker.tick(t)
	}
	select {
	case <-ticker.stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the open-turn hold outlived openTurnMaxHold")
	}
	assert.Equal(t, afterChunk+3, resets.Load(), "the hold resets the timer until its bound, then stops")
	r.finish()
}

// TestDuplexProviderStage_UserTranscriptOpensNoTurn: the caller's own
// transcript is not the provider's reply, so it holds nothing open.
func TestDuplexProviderStage_UserTranscriptOpensNoTurn(t *testing.T) {
	var resets atomic.Int64
	ctx := contextWithIdleReset(context.Background(), func() { resets.Add(1) }, 40*time.Millisecond)
	ticker := newFakeTicker()
	sess := newRecordingSession()
	s := stageWithSession(sess)
	s.idleHoldTicker = ticker.start
	r := startOpenTurnRig(t, ctx, s, sess)

	r.send(providers.StreamChunk{Delta: "hello", Metadata: map[string]interface{}{"type": "input_transcription"}})
	r.finish()
	assert.Zero(t, ticker.interval, "an input transcript started an open-turn hold")
}

// TestDuplexProviderStage_OpenTurnHoldEndsWithResponseLoop: a turn still open
// when the response loop ends leaves no heartbeat running behind it.
func TestDuplexProviderStage_OpenTurnHoldEndsWithResponseLoop(t *testing.T) {
	ctx := contextWithIdleReset(context.Background(), func() {}, 40*time.Millisecond)
	ticker := newFakeTicker()
	sess := newRecordingSession()
	s := stageWithSession(sess)
	s.idleHoldTicker = ticker.start
	r := startOpenTurnRig(t, ctx, s, sess)

	r.send(audioChunk())
	r.finish()
	select {
	case <-ticker.stopped:
	case <-time.After(idleProbeBound):
		t.Fatal("the open-turn hold outlived the response loop")
	}
}

// tick delivers one heartbeat tick, failing if the heartbeat is not running.
func (f *fakeTicker) tick(t *testing.T) {
	t.Helper()
	select {
	case f.ticks <- time.Time{}:
	case <-time.After(idleProbeBound):
		t.Fatal("no heartbeat took the tick")
	}
}
