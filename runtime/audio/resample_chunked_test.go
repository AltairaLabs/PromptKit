package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

// generateSineBytes returns durationMs of s16le mono PCM at sampleRate
// holding a clean 440 Hz sine. Anchored to absolute sample positions so a
// chunk-by-chunk caller produces the same waveform as a single call.
func generateSineBytes(durationMs, sampleRate int) []byte {
	samples := durationMs * sampleRate / 1000
	out := make([]byte, samples*2)
	const freq = 440.0
	for i := 0; i < samples; i++ {
		v := int16(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * 16000)
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}

// resampleInChunks runs ResamplePCM16 chunk-by-chunk at chunkBytes of
// input. This mirrors the production pipeline: AudioResampleStage and
// MonitorTap each call ResamplePCM16 on individual elem.Audio.Samples
// chunks, with no cross-chunk state.
func resampleInChunks(t *testing.T, input []byte, fromRate, toRate, chunkBytes int) []byte {
	t.Helper()
	out := make([]byte, 0, len(input)*toRate/fromRate)
	for off := 0; off < len(input); off += chunkBytes {
		end := off + chunkBytes
		if end > len(input) {
			end = len(input)
		}
		converted, err := ResamplePCM16(input[off:end], fromRate, toRate)
		if err != nil {
			t.Fatalf("ResamplePCM16(chunk@%d): %v", off, err)
		}
		out = append(out, converted...)
	}
	return out
}

// peakSampleDelta reports the largest absolute jump between consecutive
// samples in s16le mono PCM. A clean sine at 440 Hz / 24 kHz peaks at
// roughly |sample[i+1]-sample[i]| ≈ 0.115 × amplitude (≈1840 for our
// 16000 amplitude). Chunk-boundary discontinuities show up as sudden
// jumps an order of magnitude higher than this.
func peakSampleDelta(pcm []byte) (max int, atSampleIdx int) {
	for i := 2; i+1 < len(pcm); i += 2 {
		prev := int(int16(binary.LittleEndian.Uint16(pcm[i-2:])))
		cur := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		d := cur - prev
		if d < 0 {
			d = -d
		}
		if d > max {
			max = d
			atSampleIdx = i / 2
		}
	}
	return
}

// TestResampleChunked_RoundTripIsContinuous demonstrates whether the
// production chunk-by-chunk 24 kHz → 16 kHz → 24 kHz round-trip produces
// audible discontinuities. The pipeline puts the input through:
//
//	source  →  AudioResampleStage (24 kHz → 16 kHz)  →  MonitorTap (16 kHz → 24 kHz)
//
// each operating per-chunk. If the chain produces clean audio the peak
// sample-to-sample delta should sit within the natural rate-of-change of
// a 440 Hz sine. If chunk-boundary phase jumps are leaking through, the
// peak delta spikes far above that — which sounds like clicks at every
// chunk boundary.
func TestResampleChunked_RoundTripIsContinuous(t *testing.T) {
	const (
		srcRate    = 24000
		midRate    = 16000
		durationMs = 200
		chunkBytes = 640 // production default
	)

	input := generateSineBytes(durationMs, srcRate)

	// Reference: a clean single-shot resample.
	to16, err := ResamplePCM16(input, srcRate, midRate)
	if err != nil {
		t.Fatalf("ref ResamplePCM16: %v", err)
	}
	clean, err := ResamplePCM16(to16, midRate, srcRate)
	if err != nil {
		t.Fatalf("ref ResamplePCM16: %v", err)
	}
	cleanPeak, _ := peakSampleDelta(clean)

	// Production: per-chunk resamples on each leg.
	chunked16 := resampleInChunks(t, input, srcRate, midRate, chunkBytes)
	chunked24 := resampleInChunks(t, chunked16, midRate, srcRate, chunkBytes)
	chunkedPeak, atIdx := peakSampleDelta(chunked24)

	t.Logf("clean reference peak Δ between samples = %d", cleanPeak)
	t.Logf("chunked round-trip peak Δ between samples = %d (at sample %d)", chunkedPeak, atIdx)

	// Allow the chunked version's worst delta to be at most 4× the
	// reference: rounding error on chunk boundaries should be a few sample
	// units, not orders of magnitude. If this test fails it means the
	// per-chunk resample is producing cliffs that humans hear as clicks.
	limit := cleanPeak * 4
	if chunkedPeak > limit {
		t.Fatalf("chunked resample produces discontinuities: peak Δ=%d at sample %d, "+
			"limit=%d (clean reference Δ=%d). This is the source of the "+
			"audible clicks at chunk boundaries.",
			chunkedPeak, atIdx, limit, cleanPeak)
	}
}
