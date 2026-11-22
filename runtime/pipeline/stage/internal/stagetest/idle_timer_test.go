package stagetest

import (
	"testing"
	"time"
)

func TestFakeIdleTimer_FiresOnlyWhenAdvancedToItsDeadline(t *testing.T) {
	clock := &FakeIdleTimer{}
	fired := 0
	clock.AfterFunc(10*time.Millisecond, func() { fired++ })

	clock.Advance(10*time.Millisecond - time.Nanosecond)
	if fired != 0 {
		t.Fatalf("fired %d times before its deadline", fired)
	}
	clock.Advance(time.Nanosecond)
	if fired != 1 {
		t.Fatalf("fired %d times at its deadline, want 1", fired)
	}
	clock.Advance(time.Hour)
	if fired != 1 {
		t.Fatalf("fired %d times, want once: a fired timer stays disarmed", fired)
	}
}

func TestFakeIdleTimer_ResetMovesTheDeadlineFromNow(t *testing.T) {
	clock := &FakeIdleTimer{}
	fired := 0
	clock.AfterFunc(10*time.Millisecond, func() { fired++ })

	clock.Advance(8 * time.Millisecond)
	if was := clock.Reset(10 * time.Millisecond); !was {
		t.Fatal("Reset of an armed timer reported it was not armed")
	}
	clock.Advance(9 * time.Millisecond)
	if fired != 0 {
		t.Fatal("fired at the old deadline after a Reset")
	}
	clock.Advance(time.Millisecond)
	if fired != 1 {
		t.Fatalf("fired %d times at the reset deadline, want 1", fired)
	}
	if was := clock.Reset(time.Millisecond); was {
		t.Fatal("Reset of a fired timer reported it was armed")
	}
}

func TestFakeIdleTimer_StopDisarms(t *testing.T) {
	clock := &FakeIdleTimer{}
	fired := 0
	clock.AfterFunc(time.Millisecond, func() { fired++ })

	if was := clock.Stop(); !was {
		t.Fatal("Stop of an armed timer reported it was not armed")
	}
	if was := clock.Stop(); was {
		t.Fatal("Stop of a stopped timer reported it was armed")
	}
	clock.Advance(time.Hour)
	if fired != 0 {
		t.Fatal("a stopped timer fired")
	}
}
