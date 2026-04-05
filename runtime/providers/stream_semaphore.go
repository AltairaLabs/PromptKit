package providers

import (
	"context"

	"golang.org/x/sync/semaphore"
)

// StreamSemaphore caps the number of concurrent streaming requests a
// provider will have in flight at any one time. Acquire blocks (subject
// to context cancellation) when the limit is reached, so the caller's
// deadline controls fail-fast vs. queueing behavior: a short context
// means "reject me quickly if you're full", a long one means "queue".
//
// Rationale: per-request bounded retry + budget (Phase 1-2) does not
// bound the *number* of streams a provider can hold open. At 1000
// concurrent streams each provider holds ~1000 goroutines, timers, and
// channel buffers, even though it only needs a handful of h2 connections
// to serve them. The semaphore turns unbounded goroutine growth into
// back-pressure that surfaces cleanly at the caller.
//
// Design: wraps golang.org/x/sync/semaphore.Weighted. Nil-safe — a nil
// *StreamSemaphore never blocks and has a no-op Release, so callers can
// use it unconditionally.
type StreamSemaphore struct {
	sem   *semaphore.Weighted
	limit int64
}

// NewStreamSemaphore returns a semaphore with the given concurrent-stream
// limit. Returns nil when limit is zero or negative, which callers
// interpret as "unlimited" (no gating).
func NewStreamSemaphore(limit int) *StreamSemaphore {
	if limit <= 0 {
		return nil
	}
	l := int64(limit)
	return &StreamSemaphore{
		sem:   semaphore.NewWeighted(l),
		limit: l,
	}
}

// Acquire blocks until the semaphore has capacity or the context is
// done. Returns nil on successful acquire, or the context error on
// cancellation/deadline. A nil semaphore always returns nil immediately
// (unlimited).
//
// Callers MUST call Release exactly once for every successful Acquire,
// and MUST NOT call Release after an Acquire that returned an error.
func (s *StreamSemaphore) Acquire(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.sem.Acquire(ctx, 1)
}

// Release returns one slot to the semaphore. Nil-safe.
//
// Release of a token that was not acquired will cause semaphore.Weighted
// to panic — callers must pair each successful Acquire with exactly one
// Release, typically via defer.
func (s *StreamSemaphore) Release() {
	if s == nil {
		return
	}
	s.sem.Release(1)
}

// Limit returns the configured concurrent-stream limit. Returns 0 for a
// nil receiver (interpreted as "unlimited" by observability consumers).
func (s *StreamSemaphore) Limit() int {
	if s == nil {
		return 0
	}
	return int(s.limit)
}
