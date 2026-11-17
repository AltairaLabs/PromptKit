package audiohelper

import (
	"encoding/binary"
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
	p.jitter.Push(make([]int16, audio.DuplexRate/2))

	p.Flush()

	if got, want := p.jitter.Len(), audio.DuplexRate/100; got != want {
		t.Fatalf("jitter holds %d samples after Flush, want %d (10 ms at 48 kHz)", got, want)
	}
}
