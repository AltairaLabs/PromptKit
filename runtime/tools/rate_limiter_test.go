package tools

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRateLimiter_AllowUnlimited(t *testing.T) {
	rl := newRateLimiter(0)
	for i := 0; i < 100; i++ {
		if err := rl.Allow("tool"); err != nil {
			t.Fatalf("expected no error with unlimited rate, got: %v", err)
		}
	}
}

func TestRateLimiter_NilIsUnlimited(t *testing.T) {
	var rl *rateLimiter
	if err := rl.Allow("tool"); err != nil {
		t.Fatalf("expected nil rate limiter to allow all calls, got: %v", err)
	}
}

func TestRateLimiter_EnforcesLimit(t *testing.T) {
	rl := newRateLimiter(3)
	// Use a fixed clock so all calls are within the same window
	var now int64 = 1000000
	rl.nowFunc = func() int64 { return now }

	for i := 0; i < 3; i++ {
		if err := rl.Allow("tool_a"); err != nil {
			t.Fatalf("call %d should be allowed: %v", i+1, err)
		}
	}

	// 4th call should be rejected
	err := rl.Allow("tool_a")
	if err == nil {
		t.Fatal("expected rate limit error, got nil")
	}
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got: %v", err)
	}
}

func TestRateLimiter_PerToolIsolation(t *testing.T) {
	rl := newRateLimiter(2)
	var now int64 = 1000000
	rl.nowFunc = func() int64 { return now }

	// Fill up tool_a
	for i := 0; i < 2; i++ {
		if err := rl.Allow("tool_a"); err != nil {
			t.Fatalf("tool_a call %d should be allowed: %v", i+1, err)
		}
	}

	// tool_b should still be allowed
	if err := rl.Allow("tool_b"); err != nil {
		t.Fatalf("tool_b should be allowed: %v", err)
	}

	// tool_a should be rejected
	if err := rl.Allow("tool_a"); err == nil {
		t.Fatal("tool_a should be rate limited")
	}
}

func TestRateLimiter_WindowExpiration(t *testing.T) {
	rl := newRateLimiter(2)
	var now int64 = 1000000
	rl.nowFunc = func() int64 { return now }

	// Use up the limit
	for i := 0; i < 2; i++ {
		if err := rl.Allow("tool"); err != nil {
			t.Fatalf("call %d should be allowed: %v", i+1, err)
		}
	}

	// Should be blocked
	if err := rl.Allow("tool"); err == nil {
		t.Fatal("should be rate limited")
	}

	// Advance time past the window (60 seconds)
	now += 61_000

	// Should be allowed again
	if err := rl.Allow("tool"); err != nil {
		t.Fatalf("should be allowed after window expiry: %v", err)
	}
}

func TestRateLimiter_SetLimit(t *testing.T) {
	rl := newRateLimiter(1)
	var now int64 = 1000000
	rl.nowFunc = func() int64 { return now }

	if err := rl.Allow("tool"); err != nil {
		t.Fatalf("first call should be allowed: %v", err)
	}

	// Should be blocked at limit=1
	if err := rl.Allow("tool"); err == nil {
		t.Fatal("should be rate limited at limit=1")
	}

	// Raise the limit
	rl.SetLimit(5)

	// Should be allowed now
	if err := rl.Allow("tool"); err != nil {
		t.Fatalf("should be allowed after raising limit: %v", err)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := newRateLimiter(50)

	var allowed atomic.Int64
	var denied atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rl.Allow("tool"); err != nil {
				denied.Add(1)
			} else {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if allowed.Load() != 50 {
		t.Errorf("expected 50 allowed, got %d", allowed.Load())
	}
	if denied.Load() != 50 {
		t.Errorf("expected 50 denied, got %d", denied.Load())
	}
}
