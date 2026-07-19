package stage_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// Audio geometry: 16 kHz mono PCM16 in 100 ms chunks, so one chunk is 3200 bytes
// and a minute of audio is 600 chunks (~1.9 MB).
const (
	memBenchSampleRate   = 16000
	memBenchChunkSamples = memBenchSampleRate / 10
	memBenchChunkBytes   = memBenchChunkSamples * 2
	memBenchChunksPerMin = 600
)

// heapInUse returns current heap bytes after forcing collection twice.
//
// A single GC can leave the previous cycle's garbage uncollected, which on a
// benchmark feeding hundreds of MB is enough to swamp the signal.
func heapInUse() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// feedSilenceRetained feeds minutes of pure silence into an AudioTurnStage and
// reports the heap still held afterwards, with the stage alive and mid-turn.
//
// Feeding is instant. Turn boundaries are timed by accumulated sample count
// rather than wall-clock, so an hour of audio costs milliseconds to push through
// and retention is a deterministic function of the code — no soak test needed.
//
// The same chunk slice is reused for every element. The stage copies bytes into
// its own buffer (append(state.audioBuffer, elem.Audio.Samples...)), so what the
// measurement attributes to the heap is the stage's retention, not the feed's.
func feedSilenceRetained(b *testing.B, minutes int) uint64 {
	b.Helper()

	s, err := stage.NewAudioTurnStage(stage.DefaultAudioTurnConfig())
	if err != nil {
		b.Fatalf("NewAudioTurnStage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 1024)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Process(ctx, input, output)
	}()

	// Drain output so a full channel can never stall the feed. Pure silence
	// should emit nothing, but the drain keeps the benchmark honest if it does.
	go func() {
		for range output { //nolint:revive // draining
		}
	}()

	chunk := make([]byte, memBenchChunkBytes)
	before := heapInUse()

	for range minutes * memBenchChunksPerMin {
		input <- makeAudioElement(chunk, memBenchSampleRate)
	}

	// Measure while the stage is still holding the turn. Closing input first
	// would flush the buffer and hide exactly what we came to measure.
	after := heapInUse()

	close(input)
	<-done

	if after < before {
		return 0
	}
	return after - before
}

// BenchmarkAudioTurnStageRetention reports heap retained by a stage fed nothing
// but silence, across increasing session lengths.
//
// AudioTurnStage buffers every chunk unconditionally, and shouldCompleteTurn
// returns early while speechDetected is false — so the MaxTurnDuration bound
// never applies on this path. If retention is bounded, the reported figure stays
// flat as the minutes increase. If it scales with session length, the buffer is
// unbounded and a quiet caller grows the heap for as long as they stay quiet.
//
// Reports, does not assert. The point is to produce numbers that decide which
// sites are worth fixing, not to encode either the current behavior or a target
// as a contract.
func BenchmarkAudioTurnStageRetention(b *testing.B) {
	for _, minutes := range []int{1, 5, 30, 60} {
		b.Run(durationLabel(minutes), func(b *testing.B) {
			var retained uint64
			for range b.N {
				retained = feedSilenceRetained(b, minutes)
			}
			b.ReportMetric(float64(retained), "retained_B")
			b.ReportMetric(float64(retained)/float64(minutes), "retained_B/min")
		})
	}
}

func durationLabel(minutes int) string {
	switch minutes {
	case 1:
		return "1min"
	case 5:
		return "5min"
	case 30:
		return "30min"
	default:
		return "60min"
	}
}
