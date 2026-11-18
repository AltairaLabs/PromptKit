package audio

import (
	"encoding/binary"
	"testing"
)

func fadePCMBytes(samples ...int16) []byte {
	out := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(s))
	}
	return out
}

func fadePCMSamples(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}

// FadeOutPCM16 returns the first n samples of little-endian PCM16 audio
// ramped from full level down toward silence, for ending playback softly.
func TestFadeOutPCM16_RampsTheFirstSamples(t *testing.T) {
	in := fadePCMBytes(1000, -1000, 1000, -1000, 1000, 1000)

	got := fadePCMSamples(FadeOutPCM16(in, 4))

	want := []int16{1000, -750, 500, -250}
	if len(got) != len(want) {
		t.Fatalf("FadeOutPCM16 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FadeOutPCM16 = %v, want %v", got, want)
		}
	}
	if fadePCMSamples(in)[1] != -1000 {
		t.Error("the input was modified")
	}
}

// With fewer samples than n, the ramp spans what there is; an odd trailing
// byte is not a sample.
func TestFadeOutPCM16_ShortInput(t *testing.T) {
	in := append(fadePCMBytes(800, 800), 0x7f)

	got := fadePCMSamples(FadeOutPCM16(in, 10))

	if len(got) != 2 || got[0] != 800 || got[1] != 400 {
		t.Fatalf("FadeOutPCM16 = %v, want [800 400]", got)
	}
}
