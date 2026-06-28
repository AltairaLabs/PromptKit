package voice

import (
	"context"
	"sync"
	"testing"
)

// fakeAudioIO is a test double for AudioIO that records Play calls and supports
// Flush tracking.
type fakeAudioIO struct {
	mu         sync.Mutex
	queued     []byte
	flushCount int
	captureCh  chan []byte
}

func newFakeAudioIO() *fakeAudioIO {
	return &fakeAudioIO{captureCh: make(chan []byte)}
}

func (f *fakeAudioIO) Start(_ context.Context) error { return nil }
func (f *fakeAudioIO) CaptureChunks() <-chan []byte  { return f.captureCh }
func (f *fakeAudioIO) Play(frame []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued = append(f.queued, frame...)
}
func (f *fakeAudioIO) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued = nil
	f.flushCount++
}
func (f *fakeAudioIO) Close() error { return nil }

func (f *fakeAudioIO) PlayedAfterFlush() []byte { f.mu.Lock(); defer f.mu.Unlock(); return f.queued }
func (f *fakeAudioIO) FlushCount() int          { f.mu.Lock(); defer f.mu.Unlock(); return f.flushCount }

func TestFakeAudioIO_FlushDropsQueuedPlayback(t *testing.T) {
	io := newFakeAudioIO()
	io.Play([]byte{1, 2, 3, 4})
	io.Play([]byte{5, 6, 7, 8})
	io.Flush()
	if got := io.PlayedAfterFlush(); len(got) != 0 {
		t.Fatalf("expected no queued playback after flush, got %d bytes", len(got))
	}
	if io.FlushCount() != 1 {
		t.Fatalf("expected 1 flush, got %d", io.FlushCount())
	}
}
