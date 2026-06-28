package audio

import "sync"

// MemSink is an in-memory Sink for tests and headless use.
// It records every Write call; Flush drops the recorded frames.
// Safe for concurrent use.
type MemSink struct {
	kind    MediaKind
	mu      sync.Mutex
	written []MediaFrame
}

// NewMemSink creates a MemSink that accepts frames of the given kind.
func NewMemSink(k MediaKind) *MemSink { return &MemSink{kind: k} }

// Write appends f to the internal frame log.
func (m *MemSink) Write(f MediaFrame) { m.mu.Lock(); m.written = append(m.written, f); m.mu.Unlock() }

// Flush drops all queued frames, simulating barge-in drain.
func (m *MemSink) Flush() { m.mu.Lock(); m.written = nil; m.mu.Unlock() }

// Kind returns the MediaKind this sink accepts.
func (m *MemSink) Kind() MediaKind { return m.kind }

// Close is a no-op for MemSink; it satisfies the Sink interface.
func (m *MemSink) Close() error { return nil }

// Written returns a copy of the frames written since the last Flush.
// The returned slice is independent of the internal buffer, making it safe
// to read concurrently with ongoing Write or Flush calls.
func (m *MemSink) Written() []MediaFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MediaFrame(nil), m.written...)
}

// MemSource is an in-memory Source for tests and headless use.
// Push frames via Push; close the channel via Close so that range-loops terminate.
// Safe for concurrent use.
type MemSource struct {
	kind   MediaKind
	frames chan MediaFrame
	once   sync.Once
}

// NewMemSource creates a MemSource with a buffered channel of size buf.
func NewMemSource(kind MediaKind, buf int) *MemSource {
	return &MemSource{kind: kind, frames: make(chan MediaFrame, buf)}
}

// Push sends f onto the internal channel. It blocks if the buffer is full.
func (m *MemSource) Push(f MediaFrame) { m.frames <- f }

// Frames returns the read-only channel of MediaFrames.
func (m *MemSource) Frames() <-chan MediaFrame { return m.frames }

// Kind returns the MediaKind produced by this source.
func (m *MemSource) Kind() MediaKind { return m.kind }

// Close closes the underlying channel, signaling end-of-stream to consumers.
// It is idempotent; calling Close more than once is safe.
func (m *MemSource) Close() error { m.once.Do(func() { close(m.frames) }); return nil }
