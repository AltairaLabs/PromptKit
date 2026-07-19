package audio_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// BenchmarkResamplePCM16 measures per-chunk allocation on the resample path.
//
// ResamplePCM16 pools its []int16 scratch (resamplePool) but not the []byte it
// returns, so every chunk allocates an output slice. The same-rate case also
// allocates: it takes a full copy rather than returning the input untouched.
//
// AudioResampleStage avoids that same-rate copy via PassthroughIfSameRate
// (default true), so the SameRate figure here is the cost paid by callers using
// ResamplePCM16 directly or disabling passthrough — not by the default pipeline.
func BenchmarkResamplePCM16(b *testing.B) {
	cases := []struct {
		name       string
		from, to   int
		chunkBytes int
	}{
		// Telephony 8 kHz into the 16 kHz the STT path expects — the upsample a
		// real call performs on every chunk.
		{name: "8kTo16k", from: 8000, to: 16000, chunkBytes: benchChunkBytes / 2},
		// TTS output rate down to 16 kHz.
		{name: "24kTo16k", from: 24000, to: 16000, chunkBytes: benchChunkBytes * 3 / 2},
		// No conversion needed: measures the copy taken anyway.
		{name: "SameRate16k", from: 16000, to: 16000, chunkBytes: benchChunkBytes},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			input := make([]byte, tc.chunkBytes)

			b.ReportAllocs()
			b.SetBytes(int64(tc.chunkBytes))
			b.ResetTimer()

			for range b.N {
				out, err := audio.ResamplePCM16(input, tc.from, tc.to)
				if err != nil {
					b.Fatalf("ResamplePCM16: %v", err)
				}
				if len(out) == 0 {
					b.Fatal("empty output")
				}
			}
		})
	}
}
