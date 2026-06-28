package audio

import (
	"sync"
	"testing"
)

func TestJitterBuffer_PullOnEmpty(t *testing.T) {
	jb := NewJitterBuffer(100)
	got := jb.Pull(10)
	if len(got) != 10 {
		t.Fatalf("Pull(10) on empty: want len 10, got %d", len(got))
	}
	for i, v := range got {
		if v != 0 {
			t.Fatalf("Pull(10) on empty: sample[%d] = %d, want 0", i, v)
		}
	}
	if jb.Len() != 0 {
		t.Fatalf("Len after Pull on empty: want 0, got %d", jb.Len())
	}
}

func TestJitterBuffer_PushThenPull(t *testing.T) {
	jb := NewJitterBuffer(100)
	samples := []int16{1, 2, 3, 4, 5}
	jb.Push(samples)

	if jb.Len() != 5 {
		t.Fatalf("Len after Push(5): want 5, got %d", jb.Len())
	}

	// Pull fewer than buffered — should return exactly those samples.
	got := jb.Pull(3)
	if len(got) != 3 {
		t.Fatalf("Pull(3): want len 3, got %d", len(got))
	}
	if got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("Pull(3): want [1 2 3], got %v", got)
	}
	if jb.Len() != 2 {
		t.Fatalf("Len after Pull(3) from 5: want 2, got %d", jb.Len())
	}

	// Pull more than remaining — tail should be zero-filled.
	got = jb.Pull(5)
	if len(got) != 5 {
		t.Fatalf("Pull(5) from 2 remaining: want len 5, got %d", len(got))
	}
	if got[0] != 4 || got[1] != 5 {
		t.Fatalf("Pull(5) from 2: first two samples want [4 5], got %v", got[:2])
	}
	for i := 2; i < 5; i++ {
		if got[i] != 0 {
			t.Fatalf("Pull(5) tail zero-fill: sample[%d] = %d, want 0", i, got[i])
		}
	}
	if jb.Len() != 0 {
		t.Fatalf("Len after draining: want 0, got %d", jb.Len())
	}
}

func TestJitterBuffer_FIFO(t *testing.T) {
	jb := NewJitterBuffer(200)
	jb.Push([]int16{10, 20, 30})
	jb.Push([]int16{40, 50})

	got := jb.Pull(5)
	want := []int16{10, 20, 30, 40, 50}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("FIFO order: got[%d]=%d, want %d", i, got[i], v)
		}
	}
}

func TestJitterBuffer_Clear(t *testing.T) {
	jb := NewJitterBuffer(100)
	jb.Push([]int16{1, 2, 3, 4, 5})
	jb.Clear()

	if jb.Len() != 0 {
		t.Fatalf("Len after Clear: want 0, got %d", jb.Len())
	}
	got := jb.Pull(4)
	if len(got) != 4 {
		t.Fatalf("Pull after Clear: want len 4, got %d", len(got))
	}
	for i, v := range got {
		if v != 0 {
			t.Fatalf("Pull after Clear: sample[%d] = %d, want 0", i, v)
		}
	}
}

func TestJitterBuffer_Overflow_DropsOldest(t *testing.T) {
	cap := 5
	jb := NewJitterBuffer(cap)
	// Push 5 samples to fill the buffer exactly.
	jb.Push([]int16{1, 2, 3, 4, 5})
	if jb.Len() != 5 {
		t.Fatalf("after filling: want Len=5, got %d", jb.Len())
	}
	if jb.Drops() != 0 {
		t.Fatalf("Drops after fill: want 0, got %d", jb.Drops())
	}

	// Push 3 more; oldest 3 (1,2,3) should be dropped.
	jb.Push([]int16{6, 7, 8})
	if jb.Drops() != 3 {
		t.Fatalf("Drops after overflow Push(3): want 3, got %d", jb.Drops())
	}
	if jb.Len() != 5 {
		t.Fatalf("Len after overflow: want 5, got %d", jb.Len())
	}
	// Buffer should now be [4,5,6,7,8].
	got := jb.Pull(5)
	want := []int16{4, 5, 6, 7, 8}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("after overflow Pull: got[%d]=%d, want %d", i, got[i], v)
		}
	}
}

func TestJitterBuffer_Overflow_PushLargerThanCap(t *testing.T) {
	// Pushing a slice larger than capacity: should keep only the last cap samples.
	jb := NewJitterBuffer(3)
	jb.Push([]int16{1, 2, 3, 4, 5})
	if jb.Drops() != 2 {
		t.Fatalf("Drops for push-larger-than-cap: want 2, got %d", jb.Drops())
	}
	if jb.Len() != 3 {
		t.Fatalf("Len: want 3, got %d", jb.Len())
	}
	got := jb.Pull(3)
	want := []int16{3, 4, 5}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("got[%d]=%d, want %d", i, got[i], v)
		}
	}
}

func TestJitterBuffer_DropsAccumulate(t *testing.T) {
	jb := NewJitterBuffer(4)
	jb.Push([]int16{1, 2, 3, 4}) // fill
	jb.Push([]int16{5, 6})       // drops 1,2 → Drops=2
	jb.Push([]int16{7, 8})       // drops 3,4 → Drops=4
	if jb.Drops() != 4 {
		t.Fatalf("cumulative Drops: want 4, got %d", jb.Drops())
	}
}

func TestJitterBuffer_PullZero(t *testing.T) {
	jb := NewJitterBuffer(100)
	jb.Push([]int16{1, 2, 3})
	got := jb.Pull(0)
	if len(got) != 0 {
		t.Fatalf("Pull(0): want len 0, got %d", len(got))
	}
	if jb.Len() != 3 {
		t.Fatalf("Pull(0) must not consume samples: want Len=3, got %d", jb.Len())
	}
}

func TestJitterBuffer_ZeroCapacity(t *testing.T) {
	jb := NewJitterBuffer(0)
	// Push is a no-op (everything dropped); must not panic.
	jb.Push([]int16{1, 2, 3})
	if jb.Len() != 0 {
		t.Fatalf("zero-cap Len after Push: want 0, got %d", jb.Len())
	}
	if jb.Drops() != 3 {
		t.Fatalf("zero-cap Drops after Push(3): want 3, got %d", jb.Drops())
	}
	// Pull returns n zeros without dividing by zero.
	got := jb.Pull(4)
	if len(got) != 4 {
		t.Fatalf("zero-cap Pull(4): want len 4, got %d", len(got))
	}
	for i, v := range got {
		if v != 0 {
			t.Fatalf("zero-cap Pull(4): sample[%d] = %d, want 0", i, v)
		}
	}
	if jb.Len() != 0 {
		t.Fatalf("zero-cap Len after Pull: want 0, got %d", jb.Len())
	}
}

func TestJitterBuffer_Concurrency(t *testing.T) {
	jb := NewJitterBuffer(1000)
	var wg sync.WaitGroup
	// Producer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			jb.Push([]int16{int16(i), int16(i + 1)})
		}
	}()
	// Consumer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = jb.Pull(10)
		}
	}()
	// Clear goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			jb.Clear()
		}
	}()
	wg.Wait()
	// No race / panic is the assertion (race detector enforces correctness).
}
