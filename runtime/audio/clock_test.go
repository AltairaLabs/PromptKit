package audio

import (
	"testing"
	"time"
)

func TestSampleClock_PTS(t *testing.T) {
	c := newSampleClock(DuplexRate)
	c.advance(480)
	if got := c.pts(); got != 10*time.Millisecond {
		t.Fatalf("pts=%v want 10ms", got)
	}
}

func TestSampleClock_Accumulates(t *testing.T) {
	c := newSampleClock(DuplexRate)
	c.advance(480)
	c.advance(480)
	if got := c.pts(); got != 20*time.Millisecond {
		t.Fatalf("pts=%v want 20ms", got)
	}
}

func TestSampleClock_NonDuplexRate(t *testing.T) {
	c := newSampleClock(16000)
	c.advance(1600)
	if got := c.pts(); got != 100*time.Millisecond {
		t.Fatalf("pts=%v want 100ms", got)
	}
}
