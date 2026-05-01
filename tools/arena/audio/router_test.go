package audio

import (
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

func TestAudioRouter_RMSEmittedAtConfiguredCadence(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	rms := r.SubscribeRMS(20)

	loud := make([]int16, 480) // 20 ms at 24 kHz
	for i := range loud {
		loud[i] = 16000
	}
	stop := time.After(200 * time.Millisecond)
	var stopped atomic.Bool
	go func() {
		<-stop
		stopped.Store(true)
	}()
	for !stopped.Load() {
		r.Publish(Frame{Direction: DirectionInput, Samples: loud, Timestamp: time.Now()})
		time.Sleep(20 * time.Millisecond)
	}

	// Should have received at least 3 RMS frames (200ms / 33ms ~= 6)
	count := 0
	timeout := time.After(500 * time.Millisecond)
collect:
	for count < 6 {
		select {
		case f := <-rms:
			if f.UserLevel <= 0 {
				t.Fatalf("expected non-zero user level, got %f", f.UserLevel)
			}
			count++
		case <-timeout:
			break collect
		}
	}
	if count < 3 {
		t.Fatalf("expected >=3 RMS frames, got %d", count)
	}
}

func TestAudioRouter_CloseIsIdempotent(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	r.Close()
	// Should not panic on second close
	r.Close()
}
