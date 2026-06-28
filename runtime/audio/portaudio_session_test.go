package audio

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStart_DuplexOpenFailsFallsBackToTwoStream verifies that when the single
// duplex (1-in, 1-out) stream cannot be opened — e.g. mic and speaker are
// different devices with no shared clock — Start logs a warning and degrades to
// the two-stream half-duplex path instead of returning the duplex error. A fake
// portAudioLib drives the decision hardware-free: openDefaultStream fails only
// for the (1,1) duplex open and succeeds for the separate mic/speaker streams.
func TestStart_DuplexOpenFailsFallsBackToTwoStream(t *testing.T) {
	var (
		mu        sync.Mutex
		openCalls [][2]int32 // {numInput, numOutput} per Pa_OpenDefaultStream
	)
	fake := &portAudioLib{
		initialize:   func() int32 { return 0 },
		terminate:    func() int32 { return 0 },
		getErrorText: func(int32) string { return "device unavailable" },
		openDefaultStream: func(stream *uintptr, numInput, numOutput int32, _ uint64, _ float64, _ uint64, _, _ uintptr) int32 {
			mu.Lock()
			openCalls = append(openCalls, [2]int32{numInput, numOutput})
			mu.Unlock()
			if numInput == 1 && numOutput == 1 {
				return 1 // duplex open fails → forces fallback
			}
			*stream = 0xA0 // non-zero handle for the two separate streams
			return 0
		},
		startStream: func(uintptr) int32 { return 0 },
		stopStream:  func(uintptr) int32 { return 0 },
		closeStream: func(uintptr) int32 { return 0 },
		// Non-zero rc ends captureLoop on its first iteration so the launched
		// two-stream loops do not spin on the fake read (deterministic, no sleep).
		readStream:  func(uintptr, uintptr, uint64) int32 { return 1 },
		writeStream: func(uintptr, uintptr, uint64) int32 { return 0 },
	}

	io := newDuplexCore(fake, buildSessionConfig(nil))

	if err := io.Start(context.Background()); err != nil {
		t.Fatalf("Start after duplex fallback returned error, want nil: %v", err)
	}
	t.Cleanup(func() { _ = io.Close() }) // join the two-stream goroutines

	if io.duplex {
		t.Fatal("expected duplex=false after fallback to two-stream mode")
	}

	mu.Lock()
	calls := append([][2]int32(nil), openCalls...)
	mu.Unlock()

	if len(calls) < 3 {
		t.Fatalf("expected >=3 open calls (duplex + mic + speaker), got %d: %v", len(calls), calls)
	}
	if calls[0] != [2]int32{1, 1} {
		t.Fatalf("first open should be the duplex stream (1,1), got %v", calls[0])
	}
	// The two-stream fallback opens a mic (1,0) and a speaker (0,1).
	sawMic, sawSpeaker := false, false
	for _, c := range calls[1:] {
		switch c {
		case [2]int32{1, 0}:
			sawMic = true
		case [2]int32{0, 1}:
			sawSpeaker = true
		}
	}
	if !sawMic || !sawSpeaker {
		t.Fatalf("fallback should open mic (1,0) and speaker (0,1); calls=%v", calls)
	}

	// The fallback must allocate the two-stream buffers/channels that newDuplexCore
	// leaves nil.
	if io.inBuf == nil || io.outBuf == nil || io.playCh == nil || io.flushCh == nil {
		t.Fatal("fallback did not allocate two-stream buffers/channels")
	}
}

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
		outBuf:       make([]int16, PlaybackSampleRate*playbackWindowMs/msPerSecond),
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
	io, err := newAudioIO(buildSessionConfig(nil), false /* two-stream */)
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

	// Validate the production buffer-size formula (named constants, not raw
	// literals) reproduces the original 1600/960 frame counts. The 1600/960
	// literals are intentionally hard-coded here as the regression guard against
	// any behavior change at the defaults.
	captureFrames := cfg.captureRate / captureWindowDivisor             // 100 ms window
	playbackFrames := cfg.playbackRate * playbackWindowMs / msPerSecond // 40 ms window
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
	captureFrames := cfg.captureRate / captureWindowDivisor // 100 ms window
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
	cfg := buildSessionConfig([]SessionOption{WithPlaybackRate(DuplexRate)})

	if cfg.playbackRate != DuplexRate {
		t.Errorf("playbackRate = %d, want %d", cfg.playbackRate, DuplexRate)
	}
	playbackFrames := cfg.playbackRate * playbackWindowMs / msPerSecond // 40 ms window
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
//
// Delivery is made deterministic: the pump is started first, then the frame is
// enqueued and RECEIVED (with the assertion running every time) BEFORE io.done
// is closed. Closing done before receiving would race the pump's select (both
// done and captureCh ready ⇒ Go picks randomly), so the assertion could be
// skipped on ~50% of runs.
func TestPortaudioSource_FrameFormatReflectsConfiguredRate(t *testing.T) {
	const wantRate = 24000

	// Construct a minimal portaudioIO with captureRate set but no real device.
	io := &portaudioIO{
		captureRate: wantRate,
		captureCh:   make(chan []byte, 1),
		done:        make(chan struct{}),
	}
	src := &portaudioSource{io: io}

	// Start the pump first; io.done is still open so the only ready case is the
	// frame we enqueue, making delivery deterministic.
	frames := src.Frames()

	// Enqueue a fake PCM frame (32 bytes = 16 samples of PCM16).
	fakeFrame := make([]byte, 32)
	io.captureCh <- fakeFrame

	// Receive and assert — this MUST run every time.
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("frames channel closed before delivering the enqueued frame")
		}
		if f.Format.SampleRate != wantRate {
			t.Errorf("frame SampleRate = %d, want %d", f.Format.SampleRate, wantRate)
		}
		if f.Kind != KindAudio {
			t.Errorf("frame Kind = %v, want KindAudio", f.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the enqueued frame")
	}

	// Only now signal the pump to exit.
	close(io.done)
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
