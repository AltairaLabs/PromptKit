package sdk

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// mockCloser is a minimal io.Closer for testing shutdown behavior.
type mockCloser struct {
	closed    atomic.Bool
	closeFunc func() error
}

func (m *mockCloser) Close() error {
	m.closed.Store(true)
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestShutdownManager_RegisterAndDeregister(t *testing.T) {
	mgr := NewShutdownManager()

	c1 := &mockCloser{}
	c2 := &mockCloser{}

	if err := mgr.Register("conv-1", c1); err != nil {
		t.Fatalf("Register conv-1: %v", err)
	}
	if err := mgr.Register("conv-2", c2); err != nil {
		t.Fatalf("Register conv-2: %v", err)
	}

	if mgr.Len() != 2 {
		t.Fatalf("expected 2 tracked, got %d", mgr.Len())
	}

	mgr.Deregister("conv-1")
	if mgr.Len() != 1 {
		t.Fatalf("expected 1 tracked after deregister, got %d", mgr.Len())
	}

	// Deregistering a non-existent ID is a no-op.
	mgr.Deregister("does-not-exist")
	if mgr.Len() != 1 {
		t.Fatalf("expected 1 tracked after no-op deregister, got %d", mgr.Len())
	}
}

func TestShutdownManager_ShutdownClosesAll(t *testing.T) {
	mgr := NewShutdownManager()

	closers := make([]*mockCloser, 5)
	for i := range closers {
		closers[i] = &mockCloser{}
		if err := mgr.Register(idFromIndex(i), closers[i]); err != nil {
			t.Fatalf("Register %d: %v", i, err)
		}
	}

	ctx := context.Background()
	if err := mgr.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	for i, c := range closers {
		if !c.closed.Load() {
			t.Errorf("closer %d was not closed", i)
		}
	}

	if mgr.Len() != 0 {
		t.Errorf("expected 0 tracked after shutdown, got %d", mgr.Len())
	}
}

func TestShutdownManager_ShutdownAggregatesErrors(t *testing.T) {
	mgr := NewShutdownManager()

	errA := errors.New("close error A")
	errB := errors.New("close error B")

	_ = mgr.Register("ok", &mockCloser{})
	_ = mgr.Register("err-a", &mockCloser{closeFunc: func() error { return errA }})
	_ = mgr.Register("err-b", &mockCloser{closeFunc: func() error { return errB }})

	err := mgr.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	if !errors.Is(err, errA) {
		t.Errorf("expected error to contain errA, got: %v", err)
	}
	if !errors.Is(err, errB) {
		t.Errorf("expected error to contain errB, got: %v", err)
	}
}

func TestShutdownManager_ShutdownRespectsTimeout(t *testing.T) {
	mgr := NewShutdownManager()

	// Register a closer that blocks until cancelled.
	blocker := &mockCloser{
		closeFunc: func() error {
			time.Sleep(5 * time.Second)
			return nil
		},
	}
	_ = mgr.Register("blocking", blocker)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := mgr.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestShutdownManager_RejectAfterClosed(t *testing.T) {
	mgr := NewShutdownManager()

	_ = mgr.Shutdown(context.Background())

	err := mgr.Register("late", &mockCloser{})
	if !errors.Is(err, ErrShutdownManagerClosed) {
		t.Fatalf("expected ErrShutdownManagerClosed, got: %v", err)
	}
}

func TestShutdownManager_ShutdownEmpty(t *testing.T) {
	mgr := NewShutdownManager()

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown on empty manager: %v", err)
	}
}

func TestShutdownManager_ConcurrentRegisterDeregister(t *testing.T) {
	mgr := NewShutdownManager()

	var wg sync.WaitGroup
	const n = 100

	// Concurrently register.
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = mgr.Register(idFromIndex(i), &mockCloser{})
		}(i)
	}
	wg.Wait()

	if mgr.Len() != n {
		t.Fatalf("expected %d tracked, got %d", n, mgr.Len())
	}

	// Concurrently deregister half.
	for i := range n / 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mgr.Deregister(idFromIndex(i))
		}(i)
	}
	wg.Wait()

	if mgr.Len() != n/2 {
		t.Fatalf("expected %d tracked, got %d", n/2, mgr.Len())
	}

	// Shutdown the remainder.
	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestShutdownManager_DoubleShutdown(t *testing.T) {
	mgr := NewShutdownManager()

	_ = mgr.Register("conv-1", &mockCloser{})

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}

	// Second shutdown should be a no-op (no conversations left).
	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

func TestGracefulShutdownOnSignal(t *testing.T) {
	mgr := NewShutdownManager()

	c := &mockCloser{}
	_ = mgr.Register("conv-1", c)

	sigCh := make(chan os.Signal, 1)

	done := make(chan struct{})
	go func() {
		gracefulShutdownOnSignal(mgr, 5*time.Second, sigCh)
		close(done)
	}()

	// Send signal to trigger shutdown.
	sigCh <- syscall.SIGTERM

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("gracefulShutdownOnSignal did not return in time")
	}

	if !c.closed.Load() {
		t.Error("expected closer to be closed after signal")
	}
	if mgr.Len() != 0 {
		t.Errorf("expected 0 tracked, got %d", mgr.Len())
	}
}

func TestGracefulShutdownOnSignal_WithErrors(t *testing.T) {
	mgr := NewShutdownManager()

	_ = mgr.Register("err", &mockCloser{closeFunc: func() error {
		return errors.New("close failed")
	}})

	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGINT

	// Should complete without panicking even with close errors.
	gracefulShutdownOnSignal(mgr, 5*time.Second, sigCh)

	if mgr.Len() != 0 {
		t.Errorf("expected 0 tracked, got %d", mgr.Len())
	}
}

// idFromIndex is a helper that converts an integer index to a string ID.
func idFromIndex(i int) string {
	return "conv-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
}
