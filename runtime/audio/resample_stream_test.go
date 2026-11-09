package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// streamResample feeds input to a PCM16StreamResampler in chunks of the
// given sizes (cycled), then flushes.
func streamResample(t *testing.T, input []byte, from, to int, sizes []int) []byte {
	t.Helper()
	r, err := NewPCM16StreamResampler(from, to)
	if err != nil {
		t.Fatalf("NewPCM16StreamResampler: %v", err)
	}
	var out []byte
	for off, i := 0, 0; off < len(input); i++ {
		end := off + sizes[i%len(sizes)]
		if end > len(input) {
			end = len(input)
		}
		out = append(out, r.Write(input[off:end])...)
		off = end
	}
	return append(out, r.Flush()...)
}

// The output does not depend on how the input was chunked, odd byte counts
// included; a second of input yields exactly a second of output at the new
// rate; and every sample matches a whole-stream ResamplePCM16 to within the
// one-step rounding of its float position arithmetic.
func TestPCM16StreamResampler_MatchesWholeStreamResample(t *testing.T) {
	input := generateSineBytes(1000, SampleRate24kHz) // 24000 samples
	for _, to := range []int{8000, 16000, 22050, 44100, 48000} {
		want, err := ResamplePCM16(input, SampleRate24kHz, to)
		if err != nil {
			t.Fatalf("ResamplePCM16: %v", err)
		}
		whole := streamResample(t, input, SampleRate24kHz, to, []int{len(input)})
		if got := len(whole) / 2; got != to {
			t.Fatalf("to=%d: one second of input gave %d samples, want %d", to, got, to)
		}
		for i := 0; i < len(whole); i += 2 {
			g := int16(binary.LittleEndian.Uint16(whole[i:]))
			w := int16(binary.LittleEndian.Uint16(want[i:]))
			if d := int(g) - int(w); d < -1 || d > 1 {
				t.Fatalf("to=%d sample %d = %d, whole-stream resample has %d", to, i/2, g, w)
			}
		}
		for _, sizes := range [][]int{{4096}, {1}, {3, 4097, 2, 5}} {
			if got := streamResample(t, input, SampleRate24kHz, to, sizes); !bytes.Equal(got, whole) {
				t.Errorf("to=%d sizes=%v: output depends on chunking", to, sizes)
			}
		}
	}
}

func TestPCM16StreamResampler_RejectsNonPositiveRates(t *testing.T) {
	if _, err := NewPCM16StreamResampler(0, 16000); err == nil {
		t.Error("from=0 must be refused")
	}
	if _, err := NewPCM16StreamResampler(24000, -1); err == nil {
		t.Error("to=-1 must be refused")
	}
}
