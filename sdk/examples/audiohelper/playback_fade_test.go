package audiohelper

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
)

func le16(samples ...int16) []byte {
	out := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(s))
	}
	return out
}

// A barge-in ends playback with a short fade by default, not a hard cut.
func TestBuildSessionConfig_FadesFlushByDefault(t *testing.T) {
	if got := buildSessionConfig(nil).flushFade; got != defaultFlushFade {
		t.Fatalf("default flush fade = %v, want %v", got, defaultFlushFade)
	}
	if got := buildSessionConfig([]SessionOption{WithFlushFade(0)}).flushFade; got != 0 {
		t.Fatalf("WithFlushFade(0) = %v, want 0 (a hard cut)", got)
	}
}

// The flush hands the audio still queued for the speaker to the play loop,
// which fades its head out instead of dropping it.
func TestPortaudioIO_FlushCarriesTheQueuedAudio(t *testing.T) {
	p := &portaudioIO{playCh: make(chan []byte, 4), flushCh: make(chan []byte, 1)}
	p.playCh <- le16(1, 2)
	p.playCh <- le16(3)

	p.requestFlush()

	if got := len(p.playCh); got != 0 {
		t.Fatalf("playCh still holds %d frames", got)
	}
	if got := <-p.flushCh; string(got) != string(le16(1, 2, 3)) {
		t.Fatalf("flush carried %v, want the queued frames in order", got)
	}
}

// The tail played on a flush is the queued audio's head, ramped to silence
// over the fade, and padded with silence to whole speaker blocks.
func TestFlushTail_RampsTheQueuedHeadIntoWholeBlocks(t *testing.T) {
	acc := le16(2000, 2000)
	drained := le16(1000, 1000, 1000, 1000)

	got := flushTail(acc, drained, 4, 3)

	want := le16(2000, 1500, 500, 250, 0, 0) // the accumulation first, then what was drained
	if string(got) != string(want) {
		t.Fatalf("flushTail = %v, want %v", got, want)
	}
}

// In duplex mode the flush keeps only the fade's worth of the jitter buffer,
// ramped down.
func TestPortaudioIO_DuplexFlushFadesTheJitterBuffer(t *testing.T) {
	p := &portaudioIO{jitter: audio.NewJitterBuffer(audio.DuplexRate), flushFade: 10 * time.Millisecond}
	p.duplex.Store(true)
	queued := make([]int16, audio.DuplexRate/2)
	for i := range queued {
		queued[i] = 1000
	}
	p.jitter.Push(queued)

	p.Flush()

	n := audio.DuplexRate / 100 // 10 ms at 48 kHz
	if got := p.jitter.Len(); got != n {
		t.Fatalf("jitter holds %d samples after Flush, want %d", got, n)
	}
	got := p.jitter.Pull(n)
	for _, i := range []int{0, n / 2, n - 1} {
		if want := int16(1000 * (n - i) / n); got[i] != want {
			t.Fatalf("sample %d after Flush = %d, want %d (a linear ramp)", i, got[i], want)
		}
	}
}

// A fade is measured in samples at the stream's rate; one too short to give
// a single sample is no fade, so the flush cuts.
func TestFadeSamples(t *testing.T) {
	cases := []struct {
		rate int
		d    time.Duration
		want int
	}{
		{48000, 100 * time.Millisecond, 4800},
		{48000, 500 * time.Microsecond, 24},
		{24000, 10 * time.Microsecond, 0},
		{24000, 0, 0},
		{24000, -time.Millisecond, 0},
		{0, time.Second, 0},
	}
	for _, c := range cases {
		if got := fadeSamples(c.rate, c.d); got != c.want {
			t.Errorf("fadeSamples(%d, %v) = %d, want %d", c.rate, c.d, got, c.want)
		}
	}
}

// playLoopRecorder runs playLoop on a fake stream that records every sample
// written and every stream stop, until want samples have been written.
type playLoopRecorder struct {
	mu      sync.Mutex
	written []int16
	stops   int
}

func runPlayLoop(t *testing.T, p *portaudioIO, want int) *playLoopRecorder {
	t.Helper()
	rec := &playLoopRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	p.outStream = 0xA0
	p.done = make(chan struct{})
	p.lib = &portAudioLib{
		writeStream: func(uintptr, uintptr, uint64) int32 {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			rec.written = append(rec.written, p.outBuf...)
			if len(rec.written) >= want {
				cancel()
			}
			return 0
		},
		stopStream: func(uintptr) int32 {
			rec.mu.Lock()
			rec.stops++
			rec.mu.Unlock()
			return 0
		},
		startStream: func(uintptr) int32 { return 0 },
	}
	p.wg.Add(1)
	go p.playLoop(ctx)
	p.wg.Wait()
	cancel()
	return rec
}

// A frame queued after a flush was requested plays after the fade, never
// before it: the flush is older than the frame even when both are ready.
func TestPortaudioIO_PlayLoopTakesAPendingFlushBeforeANewerFrame(t *testing.T) {
	for i := 0; i < 50; i++ { // both channels ready: select alone picks at random
		p := &portaudioIO{
			playCh:       make(chan []byte, 1),
			flushCh:      make(chan []byte, 1),
			outBuf:       make([]int16, 2),
			playbackRate: 20,
			flushFade:    100 * time.Millisecond, // 2 samples at 20 Hz
		}
		p.flushCh <- le16(1000, 1000)
		p.playCh <- le16(300, 300)

		rec := runPlayLoop(t, p, 4)

		want := []int16{1000, 500, 300, 300}
		if len(rec.written) < len(want) {
			t.Fatalf("wrote %v, want %v", rec.written, want)
		}
		for j := range want {
			if rec.written[j] != want[j] {
				t.Fatalf("run %d: wrote %v, want the fade %v then the frame", i, rec.written, want)
			}
		}
	}
}

// A fade too short for one sample cuts: the stream restarts and nothing of
// the queued audio plays.
func TestPortaudioIO_TooShortAFadeCuts(t *testing.T) {
	p := &portaudioIO{
		playCh:       make(chan []byte, 1),
		flushCh:      make(chan []byte, 1),
		outBuf:       make([]int16, 2),
		playbackRate: 24000,
		flushFade:    10 * time.Microsecond,
	}
	p.flushCh <- le16(1000, 1000)
	p.playCh <- le16(300, 300)

	rec := runPlayLoop(t, p, 2)

	if rec.stops != 1 {
		t.Fatalf("stream stopped %d times, want 1 (a hard cut)", rec.stops)
	}
	if want := []int16{300, 300}; len(rec.written) != 2 || rec.written[0] != want[0] || rec.written[1] != want[1] {
		t.Fatalf("wrote %v, want only the newer frame %v", rec.written, want)
	}
}
