package audio_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// Audio geometry used by these benchmarks: 16 kHz mono PCM16, 100 ms chunks.
const (
	benchSampleRate   = 16000
	benchChunkSamples = benchSampleRate / 10 // 100 ms
	benchChunkBytes   = benchChunkSamples * 2
)

// quietLogger redirects slog away from stderr for the duration of a benchmark.
//
// SilenceDetector emits a slog.Warn on every chunk once its buffer is at the
// cap. Left on stderr that would both flood the output and make the measurement
// mostly a test of the logging backend. Discarding it isolates the memmove —
// note that production cost is therefore HIGHER than these numbers, not lower.
func quietLogger(b *testing.B) {
	b.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(prev) })
}

// newSpeakingDetector returns a detector already in a speaking turn, which is
// the state in which ProcessAudio accumulates (it buffers only while
// userSpeaking || hadSpeech).
func newSpeakingDetector(b *testing.B, maxBuffer int) *audio.SilenceDetector {
	b.Helper()
	d := audio.NewSilenceDetector(time.Second, audio.WithMaxAudioBufferSize(maxBuffer))
	if _, err := d.ProcessVADState(context.Background(), audio.VADStateSpeaking); err != nil {
		b.Fatalf("ProcessVADState: %v", err)
	}
	return d
}

// BenchmarkSilenceDetectorProcessAudio measures the per-chunk cost of feeding a
// speaking turn, below and at the buffer cap.
//
// SilenceDetector bounds its buffer by shifting the entire buffer down by one
// chunk on every call once full:
//
//	copy(d.audioBuffer, d.audioBuffer[excess:])
//
// Below the cap that branch never runs and the cost is a plain append. At the
// cap every chunk memmoves the whole buffer — at 100 chunks/sec/track that is
// the difference the AtCap/BelowCap ratio makes visible.
//
// This is churn, not retention: the buffer IS bounded at 10 MB, so a
// retention-only measurement would report this code as healthy.
func BenchmarkSilenceDetectorProcessAudio(b *testing.B) {
	cases := []struct {
		name string
		// maxBuffer sized so the benchmark either never reaches the cap or is
		// already sitting on it before the timer starts.
		maxBuffer int
		prefill   bool
	}{
		{name: "BelowCap", maxBuffer: audio.DefaultMaxAudioBufferSize, prefill: false},
		{name: "AtCap_10MB", maxBuffer: audio.DefaultMaxAudioBufferSize, prefill: true},
		{name: "AtCap_1MB", maxBuffer: 1024 * 1024, prefill: true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			quietLogger(b)
			ctx := context.Background()
			chunk := make([]byte, benchChunkBytes)
			d := newSpeakingDetector(b, tc.maxBuffer)

			if tc.prefill {
				// Fill past the cap so the trim branch is active from the first
				// timed iteration rather than partway through.
				for filled := 0; filled <= tc.maxBuffer; filled += benchChunkBytes {
					if _, err := d.ProcessAudio(ctx, chunk); err != nil {
						b.Fatalf("prefill ProcessAudio: %v", err)
					}
				}
			}

			b.ReportAllocs()
			b.SetBytes(benchChunkBytes)
			b.ResetTimer()

			for range b.N {
				if _, err := d.ProcessAudio(ctx, chunk); err != nil {
					b.Fatalf("ProcessAudio: %v", err)
				}
			}
		})
	}
}
