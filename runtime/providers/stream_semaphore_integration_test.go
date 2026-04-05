package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// newTestBaseProvider returns a BaseProvider wired with a fresh
// semaphore at the given limit. Metrics are registered into a dedicated
// test registry and installed as the process-wide default via
// NewStreamMetrics so AcquireStreamSlot's metric emission is observable.
func newTestBaseProvider(t *testing.T, id string, limit int) (*BaseProvider, *StreamMetrics) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := NewStreamMetrics(reg, "test", nil)

	// Install the metrics instance on the global so BaseProvider sees it.
	// This is a test hack — production code goes through
	// RegisterDefaultStreamMetrics which is idempotent. We deliberately
	// reset + re-install here so each test gets its own counter state.
	ResetDefaultStreamMetrics()
	defaultStreamMetricsMu.Lock()
	defaultStreamMetrics = m
	defaultStreamMetricsMu.Unlock()
	t.Cleanup(ResetDefaultStreamMetrics)

	b := &BaseProvider{id: id}
	b.SetStreamSemaphore(NewStreamSemaphore(limit))
	return b, m
}

// When a concurrent-stream limit is set and the caller's context is
// cancelled while waiting for a slot, AcquireStreamSlot must return the
// context error AND emit the rejection counter with reason
// "context_canceled" (not "deadline_exceeded").
func TestBaseProvider_AcquireStreamSlot_ContextCancel(t *testing.T) {
	// NOT t.Parallel(): these tests share the DefaultStreamMetrics global
	// (BaseProvider.AcquireStreamSlot reads it directly), so running them
	// in parallel would cause one test's Reset to clobber another's install.
	b, m := newTestBaseProvider(t, "openai-test", 1)

	// Drain the single slot so the next acquire must block.
	if err := b.AcquireStreamSlot(context.Background()); err != nil {
		t.Fatalf("priming acquire failed: %v", err)
	}
	defer b.ReleaseStreamSlot()

	// Start a blocked acquire and cancel the context.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- b.AcquireStreamSlot(ctx) }()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected Canceled, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("acquire did not unblock after cancel")
	}

	got := testutil.ToFloat64(m.streamConcurrencyRejections.WithLabelValues("openai-test", "context_canceled"))
	if got != 1 {
		t.Errorf("rejection counter for context_canceled = %v, want 1", got)
	}
}

// When the caller's context has a deadline and the slot acquire times
// out, the rejection must be recorded under reason "deadline_exceeded".
func TestBaseProvider_AcquireStreamSlot_DeadlineExceeded(t *testing.T) {
	// NOT t.Parallel(): these tests share the DefaultStreamMetrics global
	// (BaseProvider.AcquireStreamSlot reads it directly), so running them
	// in parallel would cause one test's Reset to clobber another's install.
	b, m := newTestBaseProvider(t, "openai-test", 1)

	// Drain the single slot.
	if err := b.AcquireStreamSlot(context.Background()); err != nil {
		t.Fatalf("priming acquire failed: %v", err)
	}
	defer b.ReleaseStreamSlot()

	// Short-deadline acquire must block and then return DeadlineExceeded.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := b.AcquireStreamSlot(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}

	got := testutil.ToFloat64(m.streamConcurrencyRejections.WithLabelValues("openai-test", "deadline_exceeded"))
	if got != 1 {
		t.Errorf("rejection counter for deadline_exceeded = %v, want 1", got)
	}
}

// A nil semaphore must never block, never emit rejection metrics, and
// must allow arbitrary concurrent acquisitions (the backwards-compat
// default for providers that don't opt into concurrency bounds).
func TestBaseProvider_AcquireStreamSlot_NilSemaphore(t *testing.T) {
	// NOT t.Parallel(): these tests share the DefaultStreamMetrics global
	// (BaseProvider.AcquireStreamSlot reads it directly), so running them
	// in parallel would cause one test's Reset to clobber another's install.
	b, m := newTestBaseProvider(t, "openai-test", 1)
	b.SetStreamSemaphore(nil) // explicit reset to unlimited

	// Acquire many times without releasing — nil semaphore should
	// never block and never reject.
	for i := 0; i < 100; i++ {
		if err := b.AcquireStreamSlot(context.Background()); err != nil {
			t.Fatalf("nil semaphore acquire %d failed: %v", i, err)
		}
	}

	// Release is also a no-op.
	for i := 0; i < 100; i++ {
		b.ReleaseStreamSlot()
	}

	// No rejection metrics should have been emitted.
	got := testutil.ToFloat64(m.streamConcurrencyRejections.WithLabelValues("openai-test", "context_canceled"))
	if got != 0 {
		t.Errorf("expected no rejections with nil semaphore, got %v", got)
	}
}

// After a successful Acquire + Release, another Acquire should succeed
// immediately (semaphore slot was properly returned).
func TestBaseProvider_AcquireStreamSlot_ReleaseReturnsSlot(t *testing.T) {
	t.Parallel()
	b, _ := newTestBaseProvider(t, "openai-test", 1)

	for i := 0; i < 5; i++ {
		if err := b.AcquireStreamSlot(context.Background()); err != nil {
			t.Fatalf("acquire iteration %d failed: %v", i, err)
		}
		b.ReleaseStreamSlot()
	}
}
