package audio_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// countingHandler counts emitted records at or above warn level.
type countingHandler struct {
	mu sync.Mutex
	n  int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.mu.Lock()
		h.n++
		h.mu.Unlock()
	}
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func (h *countingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// captureWarnings installs a counting logger for the duration of the test.
func captureWarnings(t *testing.T) *countingHandler {
	t.Helper()
	h := &countingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

// speakingDetector returns a detector already inside a speaking turn, which is
// the only state in which ProcessAudio accumulates.
func speakingDetector(t *testing.T, maxBuffer int) *audio.SilenceDetector {
	t.Helper()
	d := audio.NewSilenceDetector(time.Second, audio.WithMaxAudioBufferSize(maxBuffer))
	if _, err := d.ProcessVADState(context.Background(), audio.VADStateSpeaking); err != nil {
		t.Fatalf("ProcessVADState: %v", err)
	}
	return d
}

const trimChunkBytes = 3200 // 100 ms @ 16 kHz mono PCM16

// TestSilenceDetectorDoesNotWarnOncePerChunkAtCap covers log spam on the hot path.
//
// The buffer cap is enforced inside ProcessAudio, which runs once per audio
// chunk — roughly 100 times a second per track. Emitting a warning from that
// branch means a caller who simply keeps talking past the cap produces a
// sustained 100 warnings/sec, drowning the log and costing formatting work on
// the audio path.
//
// Trimming is routine backpressure, not an anomaly worth a per-chunk warning.
// It should be reported at most once per compaction.
func TestSilenceDetectorDoesNotWarnOncePerChunkAtCap(t *testing.T) {
	warnings := captureWarnings(t)
	ctx := context.Background()

	maxBuffer := 10 * trimChunkBytes
	d := speakingDetector(t, maxBuffer)
	chunk := make([]byte, trimChunkBytes)

	// Fill to the cap, then push 100 more chunks while sitting on it.
	const overflowChunks = 100
	for range (maxBuffer / trimChunkBytes) + overflowChunks {
		if _, err := d.ProcessAudio(ctx, chunk); err != nil {
			t.Fatalf("ProcessAudio: %v", err)
		}
	}

	// One warning per compaction is fine; one per chunk is not. Compaction can
	// only happen a handful of times across this many chunks.
	const tolerated = overflowChunks / 10
	if got := warnings.count(); got > tolerated {
		t.Errorf("emitted %d warnings across %d chunks past the cap, want <=%d; "+
			"the trim branch is warning per chunk on the audio hot path",
			got, overflowChunks, tolerated)
	}
}

// TestSilenceDetectorCapBoundsAccumulatedAudio pins the size contract that the
// trim exists to enforce. A change to how trimming is scheduled must not let the
// exposed buffer exceed the cap.
func TestSilenceDetectorCapBoundsAccumulatedAudio(t *testing.T) {
	captureWarnings(t)
	ctx := context.Background()

	maxBuffer := 10 * trimChunkBytes
	d := speakingDetector(t, maxBuffer)
	chunk := make([]byte, trimChunkBytes)

	for range 50 {
		if _, err := d.ProcessAudio(ctx, chunk); err != nil {
			t.Fatalf("ProcessAudio: %v", err)
		}
		if got := len(d.GetAccumulatedAudio()); got > maxBuffer {
			t.Fatalf("accumulated audio is %d bytes, exceeds cap %d", got, maxBuffer)
		}
	}
}

// TestSilenceDetectorRetainsMostRecentAudio pins WHICH audio survives trimming.
//
// The cap keeps the newest audio and drops the oldest — a caller who overruns
// the buffer must still get the end of what they said, not the beginning. Any
// change to the trim mechanism has to preserve both the contents and their
// order, which a size-only assertion would not catch.
func TestSilenceDetectorRetainsMostRecentAudio(t *testing.T) {
	captureWarnings(t)
	ctx := context.Background()

	const chunkSize = 100
	const capChunks = 8
	maxBuffer := capChunks * chunkSize
	d := speakingDetector(t, maxBuffer)

	// Feed chunks whose every byte is the chunk's own index, so the retained
	// window is self-identifying.
	const totalChunks = 40
	for i := range totalChunks {
		chunk := make([]byte, chunkSize)
		for j := range chunk {
			chunk[j] = byte(i)
		}
		if _, err := d.ProcessAudio(ctx, chunk); err != nil {
			t.Fatalf("ProcessAudio: %v", err)
		}
	}

	got := d.GetAccumulatedAudio()
	if len(got) != maxBuffer {
		t.Fatalf("retained %d bytes, want exactly the cap %d", len(got), maxBuffer)
	}

	// The final capChunks chunks are indices totalChunks-capChunks .. totalChunks-1.
	for i := range capChunks {
		wantByte := byte(totalChunks - capChunks + i)
		for j := range chunkSize {
			if got[i*chunkSize+j] != wantByte {
				t.Fatalf("byte %d of retained buffer is %d, want %d; "+
					"trimming lost or reordered the most recent audio",
					i*chunkSize+j, got[i*chunkSize+j], wantByte)
			}
		}
	}
}
