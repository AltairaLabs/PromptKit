package audio

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestDuplexLoop_ReadResampleEmit_PullWrite drives the single duplex loop with
// injected fake read/write funcs (no PortAudio device). It proves the seam
// behavior: each tick reads a 480-sample mic block @48 kHz, resamples it to the
// capture rate, emits it as a MediaFrame on the Source, and writes a 480-sample
// playback block pulled from the jitter buffer. The first write is the one
// pushed playback block; subsequent writes are silence (Pull underrun fill).
func TestDuplexLoop_ReadResampleEmit_PullWrite(t *testing.T) {
	const blocks = 3

	var (
		wmu    sync.Mutex
		writes [][]int16
	)
	reads := 0
	readFn := func(buf []int16) int32 {
		reads++
		if reads > blocks {
			return 1 // non-zero rc ends the loop after `blocks` reads
		}
		for i := range buf {
			buf[i] = int16(1000 + reads) // distinguishable per block
		}
		return 0
	}
	writeFn := func(buf []int16) int32 {
		cp := make([]int16, len(buf))
		copy(cp, buf)
		wmu.Lock()
		writes = append(writes, cp)
		wmu.Unlock()
		return 0
	}

	io := &portaudioIO{
		captureRate: SampleRate16kHz,
		captureCh:   make(chan []byte, captureChanBuffer),
		jitter:      NewJitterBuffer(DuplexRate / 5),
		done:        make(chan struct{}),
		readFn:      readFn,
		writeFn:     writeFn,
	}
	io.duplex.Store(true)

	// Push ONE 480-sample playback block @48 kHz BEFORE the loop runs.
	const playValue = 4321
	playBlock := make([]int16, duplexBlockFrames)
	for i := range playBlock {
		playBlock[i] = playValue
	}
	io.jitter.Push(playBlock)

	// Source pump turns captureCh bytes into PTS-stamped MediaFrames.
	src := &portaudioSource{io: io}
	frames := src.Frames()

	io.wg.Add(1)
	go io.duplexLoop(context.Background())

	// Collect exactly `blocks` emitted frames.
	got := make([]MediaFrame, 0, blocks)
	for i := 0; i < blocks; i++ {
		select {
		case f := <-frames:
			got = append(got, f)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for frame %d/%d", i+1, blocks)
		}
	}

	io.wg.Wait()   // loop returned on the 4th read (rc != 0)
	close(io.done) // stop the pump

	// --- mic-side assertions ---
	if len(got) != blocks {
		t.Fatalf("emitted %d frames, want %d", len(got), blocks)
	}
	var prev time.Duration = -1
	for i, f := range got {
		if f.Format.SampleRate != SampleRate16kHz {
			t.Errorf("frame %d SampleRate = %d, want %d", i, f.Format.SampleRate, SampleRate16kHz)
		}
		if f.Format.Channels != 1 {
			t.Errorf("frame %d Channels = %d, want 1", i, f.Format.Channels)
		}
		if f.PTS < prev {
			t.Errorf("frame %d PTS = %v decreased below previous %v", i, f.PTS, prev)
		}
		prev = f.PTS
		// 480 samples @48k resampled to 16k => 160 samples => 320 bytes.
		if len(f.Data) != 320 {
			t.Errorf("frame %d data = %d bytes, want 320 (160 samples @16k)", i, len(f.Data))
		}
	}

	// --- playback-side assertions ---
	wmu.Lock()
	defer wmu.Unlock()
	if len(writes) < blocks {
		t.Fatalf("write called %d times, want >= %d", len(writes), blocks)
	}
	// First write is the pushed playback block.
	for i, s := range writes[0] {
		if s != playValue {
			t.Fatalf("write[0][%d] = %d, want %d (pushed playback block)", i, s, playValue)
		}
	}
	// Subsequent writes are silence (jitter underrun fill).
	for w := 1; w < blocks; w++ {
		for i, s := range writes[w] {
			if s != 0 {
				t.Fatalf("write[%d][%d] = %d, want 0 (silence on underrun)", w, i, s)
			}
		}
	}
}

// TestDuplexLoop_ContextCancelStops verifies the loop honors ctx cancellation.
func TestDuplexLoop_ContextCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before the loop starts

	io := &portaudioIO{
		captureRate: SampleRate16kHz,
		captureCh:   make(chan []byte, captureChanBuffer),
		jitter:      NewJitterBuffer(DuplexRate / 5),
		done:        make(chan struct{}),
		readFn:      func(_ []int16) int32 { return 0 },
		writeFn:     func(_ []int16) int32 { return 0 },
	}
	io.duplex.Store(true)
	io.wg.Add(1)
	done := make(chan struct{})
	go func() {
		io.duplexLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("duplexLoop did not return on canceled context")
	}
}

// TestDuplexLoop_WriteErrorStopsAndDropsOnBackpressure exercises the
// write-rc-nonzero termination branch and the captureCh backpressure-drop branch
// (unbuffered captureCh with no reader forces the default case).
func TestDuplexLoop_WriteErrorStopsAndDropsOnBackpressure(t *testing.T) {
	io := &portaudioIO{
		captureRate: SampleRate16kHz,
		captureCh:   make(chan []byte), // unbuffered, no reader => drop on send
		jitter:      NewJitterBuffer(DuplexRate / 5),
		done:        make(chan struct{}),
		readFn:      func(_ []int16) int32 { return 0 },
		writeFn:     func(_ []int16) int32 { return 1 }, // non-zero rc ends the loop
	}
	io.duplex.Store(true)
	io.wg.Add(1)
	done := make(chan struct{})
	go func() {
		io.duplexLoop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("duplexLoop did not return after a write error")
	}
}

// TestDuplexPlay_ResampleErrorDropsFrame verifies duplexPlay swallows a resample
// error (invalid playback rate) without pushing to the jitter buffer.
func TestDuplexPlay_ResampleErrorDropsFrame(t *testing.T) {
	io := &portaudioIO{
		playbackRate: 0, // invalid => ResamplePCM16 returns an error
		jitter:       NewJitterBuffer(DuplexRate / 5),
		done:         make(chan struct{}),
	}
	io.duplex.Store(true)
	io.Play(make([]byte, 64))
	if got := io.jitter.Len(); got != 0 {
		t.Fatalf("jitter Len = %d, want 0 (frame dropped on resample error)", got)
	}
}

// TestDuplexPlay_ResamplesAndPushes verifies that duplex Play resamples a
// playback-rate frame up to DuplexRate and pushes the result into the jitter
// buffer (Len grows by the 48 kHz sample count).
func TestDuplexPlay_ResamplesAndPushes(t *testing.T) {
	io := &portaudioIO{
		playbackRate: SampleRate24kHz,
		jitter:       NewJitterBuffer(DuplexRate / 5),
		done:         make(chan struct{}),
	}
	io.duplex.Store(true)
	// 240 samples @24 kHz (480 bytes) -> 480 samples @48 kHz.
	frame := make([]byte, 240*bytesPerSample)
	io.Play(frame)
	if got := io.jitter.Len(); got != 480 {
		t.Fatalf("jitter Len = %d, want 480 (240 @24k upsampled to 48k)", got)
	}
}

// TestDuplexFlush_ClearsJitter verifies duplex Flush empties the jitter buffer
// (instant silence — no stream stop/start in duplex mode).
func TestDuplexFlush_ClearsJitter(t *testing.T) {
	io := &portaudioIO{
		jitter: NewJitterBuffer(DuplexRate / 5),
		done:   make(chan struct{}),
	}
	io.duplex.Store(true)
	io.jitter.Push(make([]int16, duplexBlockFrames))
	if io.jitter.Len() == 0 {
		t.Fatal("precondition: jitter should be non-empty before Flush")
	}
	io.Flush()
	if got := io.jitter.Len(); got != 0 {
		t.Fatalf("jitter Len after Flush = %d, want 0", got)
	}
}
