package sdk

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// ErrShutdownManagerClosed is returned when Register is called after Shutdown.
var ErrShutdownManagerClosed = errors.New("shutdown manager is closed; cannot register new conversations")

// ShutdownManager tracks active conversations and closes them all on shutdown.
// It is safe for concurrent use.
//
// Use [NewShutdownManager] to create an instance and [WithShutdownManager] to
// wire it into [Open] / [OpenDuplex] so that conversations are automatically
// registered and deregistered.
//
// Example:
//
//	mgr := sdk.NewShutdownManager()
//	go sdk.GracefulShutdown(mgr, 30*time.Second)
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithShutdownManager(mgr),
//	)
//	defer conv.Close() // automatically deregisters
type ShutdownManager struct {
	mu     sync.Mutex
	convs  map[string]io.Closer // conversation ID -> conversation
	closed bool
}

// NewShutdownManager creates a new ShutdownManager.
func NewShutdownManager() *ShutdownManager {
	return &ShutdownManager{
		convs: make(map[string]io.Closer),
	}
}

// Register tracks a conversation for shutdown. If the manager has already
// been shut down, it returns [ErrShutdownManagerClosed].
func (m *ShutdownManager) Register(id string, conv io.Closer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrShutdownManagerClosed
	}
	m.convs[id] = conv
	return nil
}

// Deregister removes a conversation from tracking. It is safe to call with
// an ID that was never registered or was already deregistered.
func (m *ShutdownManager) Deregister(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.convs, id)
}

// maxConcurrentClosures is the maximum number of conversations closed
// concurrently during Shutdown. This bounds goroutine creation to avoid
// resource exhaustion when thousands of conversations are tracked.
const maxConcurrentClosures = 100

// Shutdown closes all tracked conversations. It respects the context deadline,
// returning a context error if the deadline is exceeded before all conversations
// are closed. After Shutdown returns, new registrations are rejected.
//
// Concurrency is bounded by [maxConcurrentClosures] to avoid spawning an
// unbounded number of goroutines.
//
// Errors from individual Close calls are collected and returned as a combined
// error using [errors.Join].
func (m *ShutdownManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	// Snapshot and clear so we don't hold the lock during Close calls.
	snapshot := make(map[string]io.Closer, len(m.convs))
	for id, c := range m.convs {
		snapshot[id] = c
	}
	m.convs = make(map[string]io.Closer)
	m.mu.Unlock()

	if len(snapshot) == 0 {
		return nil
	}

	type result struct {
		id  string
		err error
	}

	results := make(chan result, len(snapshot))

	// Use a semaphore to bound concurrent Close calls.
	sem := make(chan struct{}, maxConcurrentClosures)
	for id, conv := range snapshot {
		go func(id string, conv io.Closer) {
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release
			results <- result{id: id, err: conv.Close()}
		}(id, conv)
	}

	var errs []error
	for range len(snapshot) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r := <-results:
			if r.err != nil {
				errs = append(errs, r.err)
			}
		}
	}

	return errors.Join(errs...)
}

// Len returns the number of currently tracked conversations.
func (m *ShutdownManager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.convs)
}

// GracefulShutdown listens for SIGTERM and SIGINT, then calls
// mgr.Shutdown with the given timeout. It is designed to be called
// as a goroutine:
//
//	go sdk.GracefulShutdown(mgr, 30*time.Second)
func GracefulShutdown(mgr *ShutdownManager, timeout time.Duration) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	gracefulShutdownOnSignal(mgr, timeout, sigCh)
}

// gracefulShutdownOnSignal waits for a signal on sigCh and then shuts down
// the manager with the given timeout. Extracted for testability.
func gracefulShutdownOnSignal(mgr *ShutdownManager, timeout time.Duration, sigCh <-chan os.Signal) {
	sig := <-sigCh
	logger.Info("Received signal, shutting down conversations", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := mgr.Shutdown(ctx); err != nil {
		logger.Error("Shutdown completed with errors", "error", err)
	} else {
		logger.Info("All conversations closed successfully")
	}
}
