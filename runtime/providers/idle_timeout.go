package providers

import (
	"errors"
	"io"
	"sync"
	"time"
)

// ErrStreamIdleTimeout is returned when no data is received within the idle timeout.
var ErrStreamIdleTimeout = errors.New("stream idle timeout: no data received")

// IdleTimeoutReader wraps an io.ReadCloser with idle timeout detection.
// If no data is read within the configured timeout, the underlying reader
// is closed, causing any blocking Read to return an error.
type IdleTimeoutReader struct {
	inner   io.ReadCloser
	timeout time.Duration
	timer   *time.Timer

	mu     sync.Mutex
	closed bool
}

// NewIdleTimeoutReader wraps the given reader with idle timeout detection.
// The timeout is reset on every successful Read that returns data.
// If the timeout fires, the underlying reader is closed to unblock any
// pending Read calls.
func NewIdleTimeoutReader(r io.ReadCloser, timeout time.Duration) *IdleTimeoutReader {
	itr := &IdleTimeoutReader{
		inner:   r,
		timeout: timeout,
	}

	itr.timer = time.AfterFunc(timeout, func() {
		itr.mu.Lock()
		defer itr.mu.Unlock()
		if !itr.closed {
			itr.closed = true
			itr.inner.Close()
		}
	})

	return itr
}

// Read reads from the underlying reader and resets the idle timer on success.
func (r *IdleTimeoutReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 {
		r.timer.Reset(r.timeout)
	}
	return n, err
}

// Close stops the idle timer and closes the underlying reader.
func (r *IdleTimeoutReader) Close() error {
	r.timer.Stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		return r.inner.Close()
	}
	return nil
}

// IsStreamIdleTimeout checks if an error is (or wraps) a stream idle timeout.
func IsStreamIdleTimeout(err error) bool {
	return errors.Is(err, ErrStreamIdleTimeout)
}
