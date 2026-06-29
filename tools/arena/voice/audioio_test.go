package voice

import (
	"context"
	"errors"
	"strings"
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

// TestPortAudioCandidatesFor verifies each OS gets sensible, ordered library
// names (the discovery list dlopen walks).
func TestPortAudioCandidatesFor(t *testing.T) {
	cases := map[string]struct {
		first    string
		contains string
	}{
		"darwin":  {first: "libportaudio.2.dylib", contains: ".dylib"},
		"linux":   {first: "libportaudio.so.2", contains: ".so"},
		"windows": {first: "portaudio.dll", contains: ".dll"},
		"freebsd": {first: "libportaudio.so.2", contains: ".so"}, // default branch
	}
	for goos, want := range cases {
		t.Run(goos, func(t *testing.T) {
			got := portAudioCandidatesFor(goos)
			if len(got) == 0 {
				t.Fatalf("no candidates for %s", goos)
			}
			if got[0] != want.first {
				t.Fatalf("%s: first candidate = %q, want %q", goos, got[0], want.first)
			}
			for _, c := range got {
				if !strings.Contains(c, want.contains) {
					t.Fatalf("%s: candidate %q missing %q", goos, c, want.contains)
				}
			}
		})
	}
}

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

func TestPortaudioIO_FlushClearsAccumulator(t *testing.T) {
	p := &portaudioIO{
		outBuf:  make([]int16, playbackFramesPerBuffer),
		playCh:  make(chan []byte, captureChanBuffer),
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	p.playCh <- make([]byte, 64)
	p.playCh <- make([]byte, 64)
	p.requestFlush()
	if got := len(p.playCh); got != 0 {
		t.Fatalf("expected playCh drained, got %d queued", got)
	}
	if got := len(p.flushCh); got != 1 {
		t.Fatalf("expected flush signal queued, got %d", got)
	}
}

// TestNewAudioIO_LoadsOrReportsMissing exercises the real purego binding. On a
// machine with PortAudio installed it must load + initialize successfully
// (proving the CGO-free FFI works); otherwise it must return errPortAudioMissing
// with actionable guidance — never crash.
func TestNewAudioIO_LoadsOrReportsMissing(t *testing.T) {
	io, err := NewAudioIO()
	if err != nil {
		if !errors.Is(err, errPortAudioMissing) {
			t.Fatalf("expected errPortAudioMissing when load fails, got: %v", err)
		}
		if !strings.Contains(err.Error(), voiceDocsURL) {
			t.Fatalf("missing-PortAudio error should link the docs (%s), got: %v", voiceDocsURL, err)
		}
		t.Skipf("PortAudio not installed on this host: %v", err)
	}
	// Loaded + Pa_Initialize succeeded — the runtime-load binding works. Close
	// terminates PortAudio; no audio device was opened (Start was never called).
	if cerr := io.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
}
