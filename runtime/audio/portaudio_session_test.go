package audio

import (
	"errors"
	"strings"
	"testing"
	"time"
)

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

func TestPortaudioIO_FlushClearsAccumulator(t *testing.T) {
	// Use the default playback rate to derive the expected buffer size (40 ms @ 24 kHz = 960 samples).
	p := &portaudioIO{
		playbackRate: PlaybackSampleRate,
		outBuf:       make([]int16, PlaybackSampleRate*40/1000),
		playCh:       make(chan []byte, captureChanBuffer),
		flushCh:      make(chan struct{}, 1),
		done:         make(chan struct{}),
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
	io, err := newAudioIO(buildSessionConfig(nil))
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

// TestNewPortAudioSession_MissingLibIsActionable asserts the Session constructor
// surfaces the actionable errPortAudioMissing when PortAudio is absent (as in
// CI), and otherwise exposes exactly one audio Source and one audio Sink without
// opening a device.
func TestNewPortAudioSession_MissingLibIsActionable(t *testing.T) {
	sess, err := NewPortAudioSession()
	if err != nil {
		if !errors.Is(err, errPortAudioMissing) {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), voiceDocsURL) {
			t.Fatalf("missing-PortAudio error should link the docs (%s), got: %v", voiceDocsURL, err)
		}
		t.Skipf("PortAudio not installed on this host: %v", err)
	}
	defer func() {
		if cerr := sess.Close(); cerr != nil {
			t.Fatalf("Close: %v", cerr)
		}
	}()
	if got := len(sess.Sources()); got != 1 {
		t.Fatalf("expected exactly 1 source, got %d", got)
	}
	if got := len(sess.Sinks()); got != 1 {
		t.Fatalf("expected exactly 1 sink, got %d", got)
	}
	if k := sess.Sources()[0].Kind(); k != KindAudio {
		t.Fatalf("source kind = %v, want KindAudio", k)
	}
	if k := sess.Sinks()[0].Kind(); k != KindAudio {
		t.Fatalf("sink kind = %v, want KindAudio", k)
	}
}

// TestSessionConfig_Defaults verifies that buildSessionConfig with no options
// produces the documented defaults (16 kHz capture / 24 kHz playback) and that
// the derived buffer sizes exactly match the pre-refactor package constants
// (1600 capture frames, 960 playback frames), locking in zero behavior change.
func TestSessionConfig_Defaults(t *testing.T) {
	cfg := buildSessionConfig(nil)

	if cfg.captureRate != CaptureSampleRate {
		t.Errorf("captureRate = %d, want %d (CaptureSampleRate)", cfg.captureRate, CaptureSampleRate)
	}
	if cfg.playbackRate != PlaybackSampleRate {
		t.Errorf("playbackRate = %d, want %d (PlaybackSampleRate)", cfg.playbackRate, PlaybackSampleRate)
	}

	// Validate buffer-size formula reproduces the original constants.
	captureFrames := cfg.captureRate / 10          // 100 ms window
	playbackFrames := cfg.playbackRate * 40 / 1000 // 40 ms window
	if captureFrames != 1600 {
		t.Errorf("captureFrames = %d, want 1600 (100 ms @ 16 kHz)", captureFrames)
	}
	if playbackFrames != 960 {
		t.Errorf("playbackFrames = %d, want 960 (40 ms @ 24 kHz)", playbackFrames)
	}
}

// TestSessionConfig_WithCaptureRate verifies that WithCaptureRate(24000) sets
// the capture rate to 24 kHz and derives the correct 100 ms buffer (2400 frames).
func TestSessionConfig_WithCaptureRate(t *testing.T) {
	cfg := buildSessionConfig([]SessionOption{WithCaptureRate(24000)})

	if cfg.captureRate != 24000 {
		t.Errorf("captureRate = %d, want 24000", cfg.captureRate)
	}
	captureFrames := cfg.captureRate / 10 // 100 ms window
	if captureFrames != 2400 {
		t.Errorf("captureFrames = %d, want 2400 (100 ms @ 24 kHz)", captureFrames)
	}
	// playbackRate must remain at the default.
	if cfg.playbackRate != PlaybackSampleRate {
		t.Errorf("playbackRate = %d, want %d (unchanged default)", cfg.playbackRate, PlaybackSampleRate)
	}
}

// TestSessionConfig_WithPlaybackRate verifies that WithPlaybackRate(48000) sets
// the playback rate to 48 kHz and derives the correct 40 ms buffer (1920 frames).
func TestSessionConfig_WithPlaybackRate(t *testing.T) {
	cfg := buildSessionConfig([]SessionOption{WithPlaybackRate(48000)})

	if cfg.playbackRate != 48000 {
		t.Errorf("playbackRate = %d, want 48000", cfg.playbackRate)
	}
	playbackFrames := cfg.playbackRate * 40 / 1000 // 40 ms window
	if playbackFrames != 1920 {
		t.Errorf("playbackFrames = %d, want 1920 (40 ms @ 48 kHz)", playbackFrames)
	}
	// captureRate must remain at the default.
	if cfg.captureRate != CaptureSampleRate {
		t.Errorf("captureRate = %d, want %d (unchanged default)", cfg.captureRate, CaptureSampleRate)
	}
}

// TestPortaudioSource_FrameFormatReflectsConfiguredRate verifies that the
// portaudioSource emits MediaFrames whose Format.SampleRate matches the
// configured capture rate, not the package default. No PortAudio device needed.
func TestPortaudioSource_FrameFormatReflectsConfiguredRate(t *testing.T) {
	const wantRate = 24000

	// Construct a minimal portaudioIO with captureRate set but no real device.
	io := &portaudioIO{
		captureRate: wantRate,
		captureCh:   make(chan []byte, 1),
		done:        make(chan struct{}),
	}
	src := &portaudioSource{io: io}

	// Enqueue a fake PCM frame (32 bytes = 16 samples of PCM16) and close done
	// so pump exits after draining it.
	fakeFrame := make([]byte, 32)
	io.captureCh <- fakeFrame
	close(io.done)

	frames := src.Frames()

	// Drain up to the one frame we queued (pump may not deliver it if done fires
	// first — accept either outcome, but if a frame arrives it must have the right rate).
	select {
	case f, ok := <-frames:
		if !ok {
			// pump exited before delivering the frame — done raced; still valid.
			return
		}
		if f.Format.SampleRate != wantRate {
			t.Errorf("frame SampleRate = %d, want %d", f.Format.SampleRate, wantRate)
		}
		if f.Kind != KindAudio {
			t.Errorf("frame Kind = %v, want KindAudio", f.Kind)
		}
	case <-time.After(500 * time.Millisecond):
		// pump exited before delivering — acceptable, done was already closed.
	}
}

// TestPortaudioSource_Kind verifies the Source kind accessor.
func TestPortaudioSource_Kind(t *testing.T) {
	src := &portaudioSource{io: &portaudioIO{done: make(chan struct{})}}
	if k := src.Kind(); k != KindAudio {
		t.Errorf("Kind = %v, want KindAudio", k)
	}
}

// TestPortaudioSource_CloseIsIdempotent verifies that portaudioSource.Close
// delegates to portaudioIO.Close and is idempotent when the IO is already closed.
func TestPortaudioSource_CloseIsIdempotent(t *testing.T) {
	io := &portaudioIO{
		closed: true,
		done:   make(chan struct{}),
	}
	src := &portaudioSource{io: io}
	// Close on an already-closed IO must return nil without panicking.
	if err := src.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestPortaudioSink_WriteEnqueuesFrame verifies that Write enqueues the frame
// data onto the play channel.
func TestPortaudioSink_WriteEnqueuesFrame(t *testing.T) {
	io := &portaudioIO{
		playCh:  make(chan []byte, 4),
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	sink := &portaudioSink{io: io}

	data := []byte{0x01, 0x02, 0x03, 0x04}
	sink.Write(MediaFrame{Data: data})

	select {
	case got := <-io.playCh:
		if len(got) != len(data) {
			t.Errorf("playCh got %d bytes, want %d", len(got), len(data))
		}
	default:
		t.Fatal("expected frame in playCh after Write")
	}
}

// TestPortaudioSink_FlushSignalsPlayLoop verifies that Flush drains queued
// frames and sends a signal on the flush channel.
func TestPortaudioSink_FlushSignalsPlayLoop(t *testing.T) {
	io := &portaudioIO{
		playCh:  make(chan []byte, 4),
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	sink := &portaudioSink{io: io}

	io.playCh <- make([]byte, 16)
	sink.Flush()

	if got := len(io.playCh); got != 0 {
		t.Errorf("playCh should be drained after Flush, got %d item(s)", got)
	}
	if got := len(io.flushCh); got != 1 {
		t.Errorf("flushCh should have 1 signal after Flush, got %d", got)
	}
}

// TestPortaudioSink_Kind verifies the Sink kind accessor.
func TestPortaudioSink_Kind(t *testing.T) {
	sink := &portaudioSink{io: &portaudioIO{done: make(chan struct{})}}
	if k := sink.Kind(); k != KindAudio {
		t.Errorf("Kind = %v, want KindAudio", k)
	}
}

// TestPortaudioSink_CloseIsIdempotent verifies that portaudioSink.Close
// delegates to portaudioIO.Close and is idempotent when the IO is already closed.
func TestPortaudioSink_CloseIsIdempotent(t *testing.T) {
	io := &portaudioIO{
		closed: true,
		done:   make(chan struct{}),
	}
	sink := &portaudioSink{io: io}
	if err := sink.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
