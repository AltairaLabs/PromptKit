package audio

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeClock is a settable clock for the playback deadline.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClockedHandler(strategy InterruptionStrategy) (*InterruptionHandler, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	h := NewInterruptionHandler(strategy, nil)
	h.now = clock.now
	return h, clock
}

func bargesIn(t *testing.T, h *InterruptionHandler) bool {
	t.Helper()
	got, err := h.ProcessVADState(context.Background(), VADStateSpeaking)
	if err != nil {
		t.Fatalf("ProcessVADState: %v", err)
	}
	return got
}

// The bot owes a reply from the end of the user's turn until the reply ends,
// so speech in that window (the model still generating) is a barge-in (#2301).
func TestInterruptionHandler_RespondingIsBargeInWindow(t *testing.T) {
	h, _ := newClockedHandler(InterruptionImmediate)

	h.BeginResponse()
	if !h.IsBotSpeaking() {
		t.Fatal("a pending reply must count as the bot speaking")
	}
	if !bargesIn(t, h) {
		t.Fatal("speech while a reply is pending must interrupt it")
	}

	h.Reset()
	h.BeginResponse()
	h.EndResponse()
	if h.IsBotSpeaking() {
		t.Fatal("EndResponse with no playback left must end the window")
	}
	if bargesIn(t, h) {
		t.Fatal("speech after the reply ended must not interrupt")
	}
}

// Delivered audio keeps playing after it is handed over, so the window lasts
// until it would have finished: speech during playback interrupts, speech after
// it does not (#2301).
func TestInterruptionHandler_PlaybackDeadline(t *testing.T) {
	h, clock := newClockedHandler(InterruptionImmediate)

	h.ExtendPlayback(2 * time.Second)
	clock.advance(time.Second)
	h.ExtendPlayback(time.Second) // queued behind the first: plays until t+3s
	clock.advance(1900 * time.Millisecond)
	if !h.IsBotSpeaking() {
		t.Fatal("queued audio extends the window; still playing at t+2.9s")
	}
	if !bargesIn(t, h) {
		t.Fatal("speech during playback must interrupt")
	}

	h.Reset()
	if h.IsBotSpeaking() {
		t.Fatal("Reset must clear the playback deadline")
	}

	h.ExtendPlayback(time.Second)
	clock.advance(time.Second)
	if h.IsBotSpeaking() {
		t.Fatal("the window must close once playback would have finished")
	}
	if bargesIn(t, h) {
		t.Fatal("speech after playback must not interrupt")
	}
}

// Playback that starts after a silence starts now, not at the stale deadline.
func TestInterruptionHandler_PlaybackStartsNowAfterGap(t *testing.T) {
	h, clock := newClockedHandler(InterruptionImmediate)

	h.ExtendPlayback(time.Second)
	clock.advance(5 * time.Second)
	h.ExtendPlayback(time.Second)
	clock.advance(900 * time.Millisecond)
	if !h.IsBotSpeaking() {
		t.Fatal("new audio must play for its own duration from now")
	}
	clock.advance(200 * time.Millisecond)
	if h.IsBotSpeaking() {
		t.Fatal("the window must close one second after the new audio")
	}
}

// EndResponse keeps the playback deadline: the reply's audio is still playing.
func TestInterruptionHandler_EndResponseKeepsPlayback(t *testing.T) {
	h, clock := newClockedHandler(InterruptionImmediate)

	h.BeginResponse()
	h.ExtendPlayback(time.Second)
	h.EndResponse()
	if !bargesIn(t, h) {
		t.Fatal("the reply's audio is still playing after its turn ended")
	}
	h.Reset()
	clock.advance(2 * time.Second)
	if h.IsBotSpeaking() {
		t.Fatal("nothing left in flight")
	}
}

// A user turn that ends while its own barge-in is still being handled begins a
// reply the Reset closing that barge-in must not discard.
func TestInterruptionHandler_ResponseBegunDuringInterruptSurvivesReset(t *testing.T) {
	h, _ := newClockedHandler(InterruptionImmediate)

	h.BeginResponse()
	if !bargesIn(t, h) {
		t.Fatal("expected a barge-in")
	}
	h.BeginResponse() // the interrupting utterance ended before TTS reset
	h.Reset()
	if !h.IsBotSpeaking() {
		t.Fatal("the reply owed to the interrupting turn must survive the Reset")
	}
	if !bargesIn(t, h) {
		t.Fatal("speech during that reply must interrupt it")
	}

	h.Reset()
	if h.IsBotSpeaking() {
		t.Fatal("a reply begun before the interrupt is closed by the Reset")
	}
}

func TestInterruptionHandler_IgnoreStrategyIgnoresResponseWindow(t *testing.T) {
	h, _ := newClockedHandler(InterruptionIgnore)
	h.BeginResponse()
	h.ExtendPlayback(time.Second)
	if bargesIn(t, h) {
		t.Fatal("the ignore strategy never interrupts")
	}
}

// With the deferred strategy, ending the reply delivers the pending interruption.
func TestInterruptionHandler_DeferredFiresOnEndResponse(t *testing.T) {
	h, _ := newClockedHandler(InterruptionDeferred)
	fired := make(chan struct{})
	h.OnInterrupt(func() { close(fired) })

	h.BeginResponse()
	if bargesIn(t, h) {
		t.Fatal("deferred must not act immediately")
	}
	h.EndResponse()
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("deferred interruption must fire when the reply ends")
	}
}

func TestInterruptionHandler_DefaultClock(t *testing.T) {
	h := &InterruptionHandler{strategy: InterruptionImmediate}
	h.ExtendPlayback(time.Hour)
	if !h.IsBotSpeaking() {
		t.Fatal("a struct-literal handler must use the wall clock")
	}
	h.ExtendPlayback(-time.Hour) // non-positive durations are ignored
	if !h.IsBotSpeaking() {
		t.Fatal("a negative duration must not shorten the window")
	}
}
