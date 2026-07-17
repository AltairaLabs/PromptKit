package audio

import (
	"encoding/binary"
	"testing"
)

// Known G.711 μ-law anchor values (ITU-T G.711, canonical Sun/ITU reference
// bit layout: sign bit is the MSB of the *wire* byte, mirrored across 0x00
// and 0x80 since 0xFF and 0x7F are the two "zero" codes, positive and
// negative zero respectively):
//
//	0xFF decodes to 0 (positive-zero / silence midpoint);
//	0x00 decodes to the largest-magnitude negative sample (-32124);
//	0x80 decodes to the largest-magnitude positive sample (+32124).
func TestDecodeMuLaw_AnchorValues(t *testing.T) {
	out := DecodeMuLaw([]byte{0xFF, 0x00, 0x80})
	if len(out) != 6 {
		t.Fatalf("expected 6 bytes (3 samples), got %d", len(out))
	}
	s0 := int16(binary.LittleEndian.Uint16(out[0:2]))
	s1 := int16(binary.LittleEndian.Uint16(out[2:4]))
	s2 := int16(binary.LittleEndian.Uint16(out[4:6]))
	if s0 != 0 {
		t.Errorf("0xFF should decode to 0, got %d", s0)
	}
	if s1 != -32124 {
		t.Errorf("0x00 should decode to -32124, got %d", s1)
	}
	if s2 != 32124 {
		t.Errorf("0x80 should decode to 32124, got %d", s2)
	}
}

// Known G.711 a-law anchor values (ITU-T G.711, canonical reference bit
// layout: the wire byte is XORed with the alternating-bit mask 0x55, and
// zero encodes to the positive-zero code 0xD5, whose reconstruction is the
// small linear-segment bias +8, not exactly 0).
func TestALaw_AnchorValues(t *testing.T) {
	zero := pcmBytes(0)
	enc := EncodeALaw(zero)
	if len(enc) != 1 || enc[0] != 0xD5 {
		t.Fatalf("EncodeALaw(0) should yield 0xD5, got %#x", enc)
	}

	out := DecodeALaw([]byte{0xD5})
	if len(out) != 2 {
		t.Fatalf("expected 2 bytes (1 sample), got %d", len(out))
	}
	s0 := int16(binary.LittleEndian.Uint16(out[0:2]))
	if s0 != 8 {
		t.Errorf("0xD5 should decode to +8, got %d", s0)
	}
}

func pcmBytes(samples ...int16) []byte {
	b := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(s))
	}
	return b
}

func TestMuLaw_RoundTrip(t *testing.T) {
	samples := []int16{0, 1000, -1000, 30000, -30000, 12345}
	back := DecodeMuLaw(EncodeMuLaw(pcmBytes(samples...)))
	for i, want := range samples {
		got := int16(binary.LittleEndian.Uint16(back[i*2:]))
		if d := int(got) - int(want); d > 400 || d < -400 {
			t.Errorf("mu-law sample %d: got %d want ~%d", i, got, want)
		}
	}
}

func TestALaw_RoundTrip(t *testing.T) {
	samples := []int16{0, 1000, -1000, 30000, -30000, 12345}
	back := DecodeALaw(EncodeALaw(pcmBytes(samples...)))
	for i, want := range samples {
		got := int16(binary.LittleEndian.Uint16(back[i*2:]))
		if d := int(got) - int(want); d > 400 || d < -400 {
			t.Errorf("a-law sample %d: got %d want ~%d", i, got, want)
		}
	}
}
