package stagetest

import (
	"testing"
	"time"
)

func TestFakeTicker_StartHandsOutTheTestDrivenTicks(t *testing.T) {
	f := NewFakeTicker()
	ticks, stop := f.Start(20 * time.Millisecond)
	if f.Interval != 20*time.Millisecond {
		t.Fatalf("Interval = %v, want the period Start was given", f.Interval)
	}
	sent := time.Unix(42, 0)
	go func() { f.Ticks <- sent }()
	if got := <-ticks; !got.Equal(sent) {
		t.Fatalf("tick = %v, want the one the test sent", got)
	}
	select {
	case <-f.Stopped:
		t.Fatal("Stopped closed before stop ran")
	default:
	}
	stop()
	select {
	case <-f.Stopped:
	default:
		t.Fatal("stop did not close Stopped")
	}
}
