package providers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Constructor and nil-safety ---

func TestNewStreamSemaphore_NilOnNonPositive(t *testing.T) {
	t.Parallel()
	cases := []int{0, -1, -100}
	for _, limit := range cases {
		if s := NewStreamSemaphore(limit); s != nil {
			t.Errorf("NewStreamSemaphore(%d) = %v, want nil", limit, s)
		}
	}
}

func TestNewStreamSemaphore_Valid(t *testing.T) {
	t.Parallel()
	s := NewStreamSemaphore(10)
	if s == nil {
		t.Fatal("NewStreamSemaphore(10) returned nil")
	}
	if got := s.Limit(); got != 10 {
		t.Errorf("Limit() = %d, want 10", got)
	}
}

func TestStreamSemaphore_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamSemaphore
	// All methods must handle nil receiver without panicking.
	if err := s.Acquire(context.Background()); err != nil {
		t.Errorf("nil Acquire should return nil, got %v", err)
	}
	s.Release() // must not panic
	if got := s.Limit(); got != 0 {
		t.Errorf("nil Limit() = %d, want 0", got)
	}
}

// --- Limit enforcement ---

func TestStreamSemaphore_LimitEnforcement(t *testing.T) {
	t.Parallel()
	s := NewStreamSemaphore(3)
	ctx := context.Background()

	// Fill the semaphore.
	for i := 0; i < 3; i++ {
		if err := s.Acquire(ctx); err != nil {
			t.Fatalf("acquire %d should succeed, got %v", i, err)
		}
	}

	// 4th acquire must block until a release happens. We use a short
	// context to prove it's actually blocking rather than succeeding.
	shortCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	err := s.Acquire(shortCtx)
	if err == nil {
		t.Error("4th acquire should have blocked past the deadline")
		s.Release() // avoid leaking
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	// Release one and confirm we can acquire again.
	s.Release()
	if err := s.Acquire(ctx); err != nil {
		t.Errorf("acquire after release should succeed, got %v", err)
	}
}

// --- Context cancellation unblocks waiters ---

func TestStreamSemaphore_ContextCancellationUnblocks(t *testing.T) {
	t.Parallel()
	s := NewStreamSemaphore(1)
	// Drain the single slot.
	_ = s.Acquire(context.Background())
	defer s.Release()

	// Now start a goroutine that tries to acquire and should block.
	ctx, cancel := context.WithCancel(context.Background())
	acquired := make(chan error, 1)
	go func() {
		acquired <- s.Acquire(ctx)
	}()

	// Give the goroutine a moment to actually block on the semaphore.
	time.Sleep(10 * time.Millisecond)
	select {
	case <-acquired:
		t.Fatal("acquire should be blocked")
	default:
	}

	// Cancel and expect the acquire to return immediately with the
	// context error.
	cancel()
	select {
	case err := <-acquired:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("acquire did not unblock after cancel")
	}
}

// --- Concurrent access under -race ---

func TestStreamSemaphore_Concurrent(t *testing.T) {
	t.Parallel()
	const limit = 10
	s := NewStreamSemaphore(limit)

	// High-water mark of concurrent holders. Must never exceed limit.
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	const workers = 100
	const opsPerWorker = 50
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < opsPerWorker; j++ {
				if err := s.Acquire(ctx); err != nil {
					t.Errorf("acquire failed: %v", err)
					return
				}
				cur := inFlight.Add(1)
				for {
					m := maxInFlight.Load()
					if cur <= m || maxInFlight.CompareAndSwap(m, cur) {
						break
					}
				}
				// Tiny critical section to let contention develop.
				time.Sleep(100 * time.Microsecond)
				inFlight.Add(-1)
				s.Release()
			}
		}()
	}
	wg.Wait()

	if m := maxInFlight.Load(); m > int32(limit) {
		t.Errorf("max concurrent holders = %d, exceeded limit %d", m, limit)
	}
	if got := inFlight.Load(); got != 0 {
		t.Errorf("leaked %d holders after all workers done", got)
	}
}
