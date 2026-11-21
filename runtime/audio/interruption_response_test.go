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

// Each turn's end owes a reply of its own: a turn that ends while an earlier
// reply is still owed, and then turns out to need none (noise transcribed to
// nothing), ends only its own, and the earlier reply is still owed until its
// own turn ends.
func TestInterruptionHandler_RepliesOwedAreCounted(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)
	h.BeginResponse() // the reply to the caller's question
	h.BeginResponse() // a cough's turn
	h.EndResponse()   // the cough transcribed to nothing
	if !h.IsBotSpeaking() {
		t.Fatal("the reply to the question is still owed")
	}
	h.EndResponse() // the reply's turn ends
	if h.IsBotSpeaking() {
		t.Fatal("nothing is owed once both have ended")
	}
	h.EndResponse() // an extra end does not go below nothing owed
	h.BeginResponse()
	if !h.IsBotSpeaking() {
		t.Fatal("a reply begun after an extra end is owed")
	}
}

// WouldInterrupt tells a stage whether speech now would barge in, without
// firing anything: only while the bot is engaged, with the immediate
// strategy, before a barge-in has fired.
func TestInterruptionHandler_WouldInterruptFiresNothing(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)
	if h.WouldInterrupt() {
		t.Fatal("an idle bot is not interrupted")
	}
	h.BeginResponse()
	if !h.WouldInterrupt() {
		t.Fatal("a bot owing a reply is interrupted")
	}
	if h.WasInterrupted() {
		t.Fatal("asking fires nothing")
	}
	h.Interrupt()
	if h.WouldInterrupt() {
		t.Fatal("a barge-in already fired is not fired again")
	}
	deferred := NewInterruptionHandler(InterruptionDeferred, nil)
	deferred.BeginResponse()
	if deferred.WouldInterrupt() {
		t.Fatal("the deferred strategy does not interrupt at once")
	}
}

// BargeIn interrupts the reply in progress and keeps the reply owed to the
// interrupting turn, whose end began it before the barge-in fired: the Reset
// that closes the interrupted reply leaves it owed.
func TestInterruptionHandler_BargeInKeepsTheInterruptingTurnsReply(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)
	h.BeginResponse() // the reply in progress
	h.BeginResponse() // the interrupting turn's end
	if !h.BargeIn() {
		t.Fatal("a reply under way is interrupted")
	}
	select {
	case <-h.Interrupted():
	default:
		t.Fatal("BargeIn fires the interruption")
	}
	h.Reset()
	if !h.IsBotSpeaking() {
		t.Fatal("the interrupting turn's reply is still owed after the Reset")
	}

	idle := NewInterruptionHandler(InterruptionImmediate, nil)
	if idle.BargeIn() {
		t.Fatal("with no reply under way there is nothing to barge in on")
	}
	idle.Reset()
	if idle.IsBotSpeaking() {
		t.Fatal("with no reply owed, none survives the Reset")
	}
}

// A reply begun during an interruption and ended before its Reset is not
// still owed after the Reset.
func TestInterruptionHandler_ReplyEndedBeforeTheResetIsNotKept(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)
	h.Interrupt()
	h.BeginResponse()
	h.EndResponse() // e.g. the turn transcribed to nothing
	h.Reset()
	if h.IsBotSpeaking() {
		t.Fatal("no reply is owed after the Reset")
	}
}

// BargeIn interrupts only a reply still under way: once the earlier reply has
// ended and played out, the turn being transcribed is the only reply owed,
// and there is nothing to interrupt.
func TestInterruptionHandler_BargeInAfterTheReplyEndedFiresNothing(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)
	h.BeginResponse() // the earlier reply
	h.BeginResponse() // the turn now being transcribed
	h.EndResponse()   // the earlier reply's turn ended meanwhile
	if h.BargeIn() {
		t.Fatal("no reply is under way to interrupt")
	}
	if h.WasInterrupted() {
		t.Fatal("nothing fired")
	}
	if !h.IsBotSpeaking() {
		t.Fatal("the transcribed turn's reply is still owed")
	}
}

// BargeIn interrupts a reply that is only playing (its turn over, no reply
// owed), and then nothing is owed after the Reset; it never fires with the
// deferred strategy, which waits for a sentence boundary.
func TestInterruptionHandler_BargeInOnPlaybackAlone(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)
	h.ExtendPlayback(time.Minute) // the reply is playing; nothing is owed
	if !h.BargeIn() {
		t.Fatal("a reply still playing is interrupted")
	}
	h.Reset()
	if h.IsBotSpeaking() {
		t.Fatal("no reply is owed after the Reset")
	}

	deferred := NewInterruptionHandler(InterruptionDeferred, nil)
	deferred.BeginResponse()
	deferred.BeginResponse()
	if deferred.BargeIn() {
		t.Fatal("the deferred strategy does not barge in at once")
	}
}
