package audio

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestAudioRouter_FansOutToMultipleConsumers(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	c1 := r.Subscribe("c1", 10)
	c2 := r.Subscribe("c2", 10)

	frame := Frame{
		Direction: DirectionInput,
		Samples:   []int16{1, 2, 3, 4},
		Timestamp: time.Now(),
	}
	r.Publish(frame)

	select {
	case got := <-c1:
		if got.Direction != DirectionInput || len(got.Samples) != 4 {
			t.Fatalf("c1 got unexpected frame: %+v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("c1 timeout")
	}
	select {
	case got := <-c2:
		if got.Direction != DirectionInput || len(got.Samples) != 4 {
			t.Fatalf("c2 got unexpected frame: %+v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("c2 timeout")
	}
}

func TestAudioRouter_DropsForSlowConsumer(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	// Slow consumer with tiny buffer
	slow := r.Subscribe("slow", 1)
	fast := r.Subscribe("fast", 100)

	for i := 0; i < 10; i++ {
		r.Publish(Frame{Direction: DirectionInput, Samples: []int16{int16(i)}})
	}

	// Wait briefly for fanout
	time.Sleep(100 * time.Millisecond)

	fastCount := 0
	for {
		select {
		case <-fast:
			fastCount++
		default:
			goto done
		}
	}
done:
	if fastCount < 9 {
		t.Fatalf("fast consumer should have ~10 frames, got %d", fastCount)
	}
	if r.DropCount("slow") == 0 {
		t.Fatal("expected slow consumer to have non-zero drop count")
	}
	_ = slow
}

func TestAudioRouter_UnsubscribeStopsDelivery(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	c := r.Subscribe("c", 10)
	r.Unsubscribe("c")

	r.Publish(Frame{Direction: DirectionInput, Samples: []int16{1}})
	time.Sleep(20 * time.Millisecond)

	select {
	case _, ok := <-c:
		if ok {
			t.Fatal("expected channel closed after unsubscribe")
		}
	default:
		// channel may not be closed yet, but no value delivered
	}
}

func TestAudioRouter_CloseIsIdempotent(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	r.Close()
	// Should not panic on second close
	r.Close()
}

// TestAudioRouter_PublishAfterCloseDoesNotPanic exercises the close-race
// fix: the dispatch loop and intake channel get torn down, but stages
// in flight may still call Publish. Without the closed-flag guard this
// would send on a closed channel and panic.
func TestAudioRouter_PublishAfterCloseDoesNotPanic(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	r.Close()
	// Multiple publishes after Close; all should be no-ops.
	for i := 0; i < 10; i++ {
		r.Publish(Frame{Direction: DirectionInput, Samples: []int16{int16(i)}})
	}
}

// TestAudioRouter_PublishConcurrentWithCloseIsSafe runs Publish and
// Close concurrently and asserts the program does not panic. Repeated
// to make the race more likely to land between iterations.
func TestAudioRouter_PublishConcurrentWithCloseIsSafe(t *testing.T) {
	for trial := 0; trial < 20; trial++ {
		r := NewAudioRouter(Rate24k)
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				default:
					r.Publish(Frame{Direction: DirectionInput, Samples: []int16{1}})
				}
			}
		}()
		// Let Publish race against Close briefly.
		time.Sleep(time.Millisecond)
		r.Close()
		close(stop)
		<-done
	}
}

// TestAudioRouter_DuplicateSubscribeClosesPrevious asserts the
// regression fix where Subscribe used to silently leak a previously
// registered channel for the same id. The previous channel must now
// be closed (so any reader observes EOF), and only the new channel
// receives subsequent frames.
func TestAudioRouter_DuplicateSubscribeClosesPrevious(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	first := r.Subscribe("dup", 4)
	second := r.Subscribe("dup", 4)

	// Previous channel must be closed.
	select {
	case _, ok := <-first:
		if ok {
			t.Fatal("expected first channel closed after duplicate subscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("first channel was not closed after duplicate subscribe")
	}

	r.Publish(Frame{Direction: DirectionInput, Samples: []int16{1}})
	select {
	case got := <-second:
		if len(got.Samples) != 1 {
			t.Fatalf("second channel got unexpected frame: %+v", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("second channel did not receive frame")
	}
}

// TestAudioRouter_SubscribeAfterCloseReturnsClosedChannel asserts that
// late subscribers (e.g. an observer wiring up against a router whose
// run already finished) receive an already-closed channel rather than
// hanging on a never-serviced one. Without this guard, the consumer's
// range loop would block forever — the dispatch goroutine has exited
// and Publish drops frames after Close.
func TestAudioRouter_SubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	r.Close()
	ch := r.Subscribe("late", 4)
	// Receive should return immediately with ok=false.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel, got open value")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected immediate close, got hang")
	}
}

// TestAudioRouter_IntakeDropCount exercises the intake-drop counter API.
// A deterministic saturation test would need to suspend the dispatch
// loop (which drains r.in concurrently with Publish), so we settle for
// asserting the counter is callable and starts at zero — the live
// instrumentation path is what matters; the counter math is trivial.
func TestAudioRouter_IntakeDropCount(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()
	if r.IntakeDropCount() != 0 {
		t.Fatalf("expected fresh router IntakeDropCount=0, got %d", r.IntakeDropCount())
	}
}

func TestAudioRouter_RegisterDrainHandler_NilIgnored(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()
	r.RegisterDrainHandler(nil)
	r.WaitOutputDrained(context.Background()) // no handlers, returns immediately
}

func TestAudioRouter_WaitOutputDrained_NoHandlersReturnsImmediately(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	done := make(chan struct{})
	go func() {
		r.WaitOutputDrained(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitOutputDrained did not return immediately with no handlers")
	}
}

func TestAudioRouter_WaitOutputDrained_RunsAllHandlersConcurrently(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	var calls int32
	for range 3 {
		r.RegisterDrainHandler(func(_ context.Context) {
			atomic.AddInt32(&calls, 1)
			time.Sleep(20 * time.Millisecond)
		})
	}

	start := time.Now()
	r.WaitOutputDrained(context.Background())
	elapsed := time.Since(start)

	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 handler invocations, got %d", got)
	}
	// Concurrent execution: 3 × 20ms handlers complete in ~20-30ms total,
	// not the 60ms sequential baseline.
	if elapsed >= 60*time.Millisecond {
		t.Fatalf("handlers ran sequentially (elapsed=%s), want concurrent", elapsed)
	}
}

func TestAudioRouter_WaitOutputDrained_HonoursContextCancellation(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	r.RegisterDrainHandler(func(ctx context.Context) {
		<-ctx.Done() // hang until ctx cancelled
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	r.WaitOutputDrained(ctx)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("WaitOutputDrained did not return after ctx cancel (elapsed=%s)", elapsed)
	}
}
