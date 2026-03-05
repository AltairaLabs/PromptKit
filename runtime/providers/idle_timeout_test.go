package providers

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// nopCloser wraps a reader with a no-op Close method.
type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

// slowReader blocks on Read until the provided channel is closed or a value is sent.
type slowReader struct {
	dataCh chan []byte
	mu     sync.Mutex
	closed bool
}

func newSlowReader() *slowReader {
	return &slowReader{dataCh: make(chan []byte, 10)}
}

func (r *slowReader) Read(p []byte) (int, error) {
	data, ok := <-r.dataCh
	if !ok {
		return 0, io.EOF
	}
	n := copy(p, data)
	return n, nil
}

func (r *slowReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		close(r.dataCh)
	}
	return nil
}

func TestIdleTimeoutReader_NormalRead(t *testing.T) {
	data := "hello world"
	inner := nopCloser{strings.NewReader(data)}
	reader := NewIdleTimeoutReader(inner, 5*time.Second)
	defer reader.Close()

	buf := make([]byte, len(data))
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes, got %d", len(data), n)
	}
	if string(buf[:n]) != data {
		t.Fatalf("expected %q, got %q", data, string(buf[:n]))
	}
}

func TestIdleTimeoutReader_TimeoutClosesBody(t *testing.T) {
	sr := newSlowReader()
	reader := NewIdleTimeoutReader(sr, 50*time.Millisecond)
	defer reader.Close()

	// Read should block and then fail when idle timeout fires and closes the body
	buf := make([]byte, 1024)
	_, err := reader.Read(buf)
	if err == nil {
		t.Fatal("expected error from idle timeout, got nil")
	}
}

func TestIdleTimeoutReader_ResetOnData(t *testing.T) {
	sr := newSlowReader()
	reader := NewIdleTimeoutReader(sr, 100*time.Millisecond)
	defer reader.Close()

	// Send data before timeout fires - this should reset the timer
	go func() {
		time.Sleep(50 * time.Millisecond)
		sr.dataCh <- []byte("chunk1")
		time.Sleep(50 * time.Millisecond)
		sr.dataCh <- []byte("chunk2")
		time.Sleep(50 * time.Millisecond)
		sr.dataCh <- []byte("chunk3")
		time.Sleep(50 * time.Millisecond)
		sr.Close()
	}()

	var chunks []string
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunks = append(chunks, string(buf[:n]))
		}
		if err != nil {
			break
		}
	}

	// Should have received all 3 chunks because each arrived within timeout
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "chunk1" || chunks[1] != "chunk2" || chunks[2] != "chunk3" {
		t.Fatalf("unexpected chunks: %v", chunks)
	}
}

func TestIdleTimeoutReader_CloseStopsTimer(t *testing.T) {
	inner := nopCloser{strings.NewReader("data")}
	reader := NewIdleTimeoutReader(inner, 5*time.Second)

	// Close should not panic and should stop the timer
	err := reader.Close()
	if err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}

	// Double close should not panic
	err = reader.Close()
	if err != nil {
		t.Fatalf("unexpected error on double close: %v", err)
	}
}

func TestIdleTimeoutReader_MultipleReads(t *testing.T) {
	data := "abcdefghijklmnop"
	inner := nopCloser{bytes.NewReader([]byte(data))}
	reader := NewIdleTimeoutReader(inner, 5*time.Second)
	defer reader.Close()

	// Read in small chunks
	var result []byte
	buf := make([]byte, 4)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if string(result) != data {
		t.Fatalf("expected %q, got %q", data, string(result))
	}
}

func TestIdleTimeoutReader_ZeroBytesDoNotReset(t *testing.T) {
	// A reader that returns 0 bytes should not reset the timer
	sr := newSlowReader()
	reader := NewIdleTimeoutReader(sr, 50*time.Millisecond)
	defer reader.Close()

	// Don't send any data - the timer should fire
	buf := make([]byte, 1024)
	start := time.Now()
	_, err := reader.Read(buf)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from idle timeout, got nil")
	}
	// Should have timed out around 50ms
	if elapsed > 500*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestDefaultStreamIdleTimeout(t *testing.T) {
	if DefaultStreamIdleTimeout != 30*time.Second {
		t.Fatalf("expected DefaultStreamIdleTimeout to be 30s, got %v", DefaultStreamIdleTimeout)
	}
}

func TestIsStreamIdleTimeout(t *testing.T) {
	if !IsStreamIdleTimeout(ErrStreamIdleTimeout) {
		t.Fatal("IsStreamIdleTimeout should return true for ErrStreamIdleTimeout")
	}

	wrapped := errors.New("wrapped: " + ErrStreamIdleTimeout.Error())
	if IsStreamIdleTimeout(wrapped) {
		// Not wrapped with %w, so should not match
	}

	if IsStreamIdleTimeout(errors.New("other error")) {
		t.Fatal("IsStreamIdleTimeout should return false for other errors")
	}

	if IsStreamIdleTimeout(nil) {
		t.Fatal("IsStreamIdleTimeout should return false for nil")
	}
}

// closerTracker tracks whether Close was called
type closerTracker struct {
	io.Reader
	closed bool
}

func (c *closerTracker) Close() error {
	c.closed = true
	return nil
}

func TestIdleTimeoutReader_InnerCloseCalledOnTimeout(t *testing.T) {
	sr := newSlowReader()
	reader := NewIdleTimeoutReader(sr, 50*time.Millisecond)

	// Wait for timeout to fire
	time.Sleep(100 * time.Millisecond)

	// The inner reader should have been closed by the timer
	sr.mu.Lock()
	closed := sr.closed
	sr.mu.Unlock()
	if !closed {
		t.Fatal("expected inner reader to be closed after idle timeout")
	}

	// Explicit close should not panic (double close)
	reader.Close()
}

func TestIdleTimeoutReader_InnerCloseCalledOnExplicitClose(t *testing.T) {
	tracker := &closerTracker{Reader: strings.NewReader("data")}
	reader := NewIdleTimeoutReader(tracker, 5*time.Second)

	err := reader.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !tracker.closed {
		t.Fatal("expected inner reader Close to be called")
	}
}
