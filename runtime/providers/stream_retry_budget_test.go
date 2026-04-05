package providers

import (
	"sync"
	"testing"
	"time"
)

// --- Constructor and nil-safety ---

func TestNewRetryBudget_NilOnZero(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		rate  float64
		burst int
	}{
		{"zero rate", 0, 10},
		{"zero burst", 5, 0},
		{"negative rate", -1, 10},
		{"negative burst", 5, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if b := NewRetryBudget(tc.rate, tc.burst); b != nil {
				t.Errorf("NewRetryBudget(%v, %d) = %v, want nil", tc.rate, tc.burst, b)
			}
		})
	}
}

func TestNewRetryBudget_Valid(t *testing.T) {
	t.Parallel()
	b := NewRetryBudget(5, 10)
	if b == nil {
		t.Fatal("NewRetryBudget(5, 10) returned nil")
	}
	if got := b.Burst(); got != 10 {
		t.Errorf("Burst() = %d, want 10", got)
	}
	if got := b.RatePerSec(); got != 5 {
		t.Errorf("RatePerSec() = %v, want 5", got)
	}
}

func TestRetryBudget_NilSafe(t *testing.T) {
	t.Parallel()
	var b *RetryBudget
	// All methods must handle nil receiver without panicking.
	if !b.TryAcquire() {
		t.Error("nil budget TryAcquire should return true (unlimited)")
	}
	if got := b.Available(); got != 0 {
		t.Errorf("nil budget Available() = %v, want 0", got)
	}
	if got := b.Burst(); got != 0 {
		t.Errorf("nil budget Burst() = %d, want 0", got)
	}
	if got := b.RatePerSec(); got != 0 {
		t.Errorf("nil budget RatePerSec() = %v, want 0", got)
	}
}

// --- Burst enforcement ---

func TestRetryBudget_BurstEnforcement(t *testing.T) {
	t.Parallel()
	// Rate low enough that refill during the test is negligible.
	b := NewRetryBudget(0.001, 5)

	// Should allow exactly burst tokens before rejecting.
	for i := 0; i < 5; i++ {
		if !b.TryAcquire() {
			t.Fatalf("attempt %d: expected success within burst, got rejection", i)
		}
	}
	// 6th attempt should be rejected.
	if b.TryAcquire() {
		t.Error("6th acquire should be rejected after burst exhausted")
	}
}

// --- Refill ---

func TestRetryBudget_Refill(t *testing.T) {
	t.Parallel()
	// 100 tokens/sec = 10ms per token. Burst 1 means we have exactly 1
	// token initially, then must wait ~10ms for the next.
	b := NewRetryBudget(100, 1)

	if !b.TryAcquire() {
		t.Fatal("first acquire should succeed with full burst")
	}
	if b.TryAcquire() {
		t.Fatal("second acquire immediately after should fail")
	}

	// Wait long enough for refill.
	time.Sleep(20 * time.Millisecond)

	if !b.TryAcquire() {
		t.Error("acquire after refill window should succeed")
	}
}

// --- Concurrent access (race detector) ---

func TestRetryBudget_Concurrent(t *testing.T) {
	t.Parallel()
	b := NewRetryBudget(1000, 100)

	var wg sync.WaitGroup
	const goroutines = 50
	const perGoroutine = 20
	acquired := make(chan bool, goroutines*perGoroutine)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				acquired <- b.TryAcquire()
			}
		}()
	}
	wg.Wait()
	close(acquired)

	// Count successes. We cannot assert an exact number because refill
	// happens during the test, but it must be at least the burst size
	// and at most goroutines*perGoroutine.
	var successes int
	for ok := range acquired {
		if ok {
			successes++
		}
	}
	if successes < 100 {
		t.Errorf("concurrent acquires: got %d successes, expected at least burst=100", successes)
	}
	if successes > goroutines*perGoroutine {
		t.Errorf("concurrent acquires: got %d successes, max possible is %d", successes, goroutines*perGoroutine)
	}
}

// --- Available() for gauge ---

func TestRetryBudget_AvailableDecreasesOnAcquire(t *testing.T) {
	t.Parallel()
	b := NewRetryBudget(0.001, 10) // low rate, high burst
	initial := b.Available()
	if initial < 9 || initial > 10 {
		t.Fatalf("initial Available() = %v, want ~10", initial)
	}
	for i := 0; i < 5; i++ {
		b.TryAcquire()
	}
	after := b.Available()
	// At 0.001 tokens/sec the refill during 5 microsecond-scale acquires
	// is effectively zero, so the decrement should be very close to 5.
	// Tolerance of 0.1 allows for scheduling jitter without letting a
	// broken implementation (no decrement) slip through.
	if delta := initial - after; delta < 4.9 || delta > 5.1 {
		t.Errorf("Available() after 5 acquires: delta = %v, want ~5 (±0.1)", delta)
	}
}
