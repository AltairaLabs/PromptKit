package audio

import "encoding/binary"

// G.711 telephony codec (mu-law / a-law) <-> little-endian PCM16. Pure
// computation; the standard companding used by SIP/PSTN/Twilio/Talkdesk.

const (
	bytesPerSample = 2 // little-endian PCM16 = 2 bytes per sample

	muLawBias = 0x84  // 132, per ITU-T G.711
	muLawClip = 32635 // largest magnitude before mu-law clipping
	aLawClip  = 32635 // largest magnitude before a-law clipping

	signBit      = 0x80 // sign bit in the wire byte (mu-law and a-law)
	segmentMask  = 0x07 // 3-bit segment/exponent value once shifted into place
	mantissaMask = 0x0F // 4-bit mantissa mask (low nibble)
	nibbleShift  = 4    // bit width of the segment/mantissa nibbles

	maxSegmentExponent = 7      // largest 3-bit segment exponent
	topSegmentBit      = 0x4000 // starting probe bit when searching for the segment exponent
	mantissaBitShift   = 3      // bits below the mantissa field in the raw sample magnitude

	aLawToggleMask      = 0x55  // alternating-bit mask XORed into every a-law wire byte
	aLawExpMask         = 0x70  // 3-bit exponent field, still in bits 4-6
	aLawSegmentBias     = 8     // a-law: linear bias for the lowest (exponent-0) segment
	aLawSegmentBase     = 0x100 // a-law: base added once above the lowest segment
	aLawLinearThreshold = 256   // below this magnitude, a-law encodes the linear segment directly
)

// DecodeMuLaw converts G.711 mu-law bytes to little-endian PCM16 (2 bytes/sample).
func DecodeMuLaw(mulaw []byte) []byte {
	out := make([]byte, len(mulaw)*bytesPerSample)
	for i, u := range mulaw {
		//nolint:gosec // decodeMuLawSample is bounded to +-32124, fits uint16 bit pattern
		binary.LittleEndian.PutUint16(out[i*bytesPerSample:], uint16(decodeMuLawSample(u)))
	}
	return out
}

// EncodeMuLaw converts little-endian PCM16 to G.711 mu-law bytes (1 byte/sample).
func EncodeMuLaw(pcm16le []byte) []byte {
	n := len(pcm16le) / bytesPerSample
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		//nolint:gosec // reinterpreting PCM16 wire bytes as a signed sample; same bit pattern
		out[i] = encodeMuLawSample(int16(binary.LittleEndian.Uint16(pcm16le[i*bytesPerSample:])))
	}
	return out
}

func decodeMuLawSample(u byte) int16 {
	u = ^u // stored inverted
	sign := u & signBit
	exponent := (u >> nibbleShift) & segmentMask
	mantissa := u & mantissaMask
	sample := (int(mantissa) << mantissaBitShift) + muLawBias
	sample <<= exponent
	sample -= muLawBias
	if sign != 0 {
		sample = -sample
	}
	return int16(sample) //nolint:gosec // G.711 companding bounds the magnitude to +-32124, fits int16
}

func encodeMuLawSample(sample int16) byte {
	sign := 0
	s := int(sample)
	if s < 0 {
		sign = signBit
		s = -s
	}
	if s > muLawClip {
		s = muLawClip
	}
	s += muLawBias
	exponent := maxSegmentExponent
	for mask := topSegmentBit; s&mask == 0 && exponent > 0; mask >>= 1 {
		exponent--
	}
	mantissa := (s >> (exponent + mantissaBitShift)) & mantissaMask
	return byte(^(sign | (exponent << nibbleShift) | mantissa)) //nolint:gosec // result is masked to one byte
}

// DecodeALaw converts G.711 a-law bytes to little-endian PCM16 (2 bytes/sample).
func DecodeALaw(alaw []byte) []byte {
	out := make([]byte, len(alaw)*bytesPerSample)
	for i, a := range alaw {
		//nolint:gosec // decodeALawSample is bounded to +-32124, fits uint16 bit pattern
		binary.LittleEndian.PutUint16(out[i*bytesPerSample:], uint16(decodeALawSample(a)))
	}
	return out
}

// EncodeALaw converts little-endian PCM16 to G.711 a-law bytes (1 byte/sample).
func EncodeALaw(pcm16le []byte) []byte {
	n := len(pcm16le) / bytesPerSample
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		//nolint:gosec // reinterpreting PCM16 wire bytes as a signed sample; same bit pattern
		out[i] = encodeALawSample(int16(binary.LittleEndian.Uint16(pcm16le[i*bytesPerSample:])))
	}
	return out
}

func decodeALawSample(a byte) int16 {
	a ^= aLawToggleMask
	sign := a & signBit
	exponent := (a & aLawExpMask) >> nibbleShift
	mantissa := int(a & mantissaMask)
	sample := (mantissa << nibbleShift) + aLawSegmentBias
	if exponent != 0 {
		sample += aLawSegmentBase
		sample <<= exponent - 1
	}
	if sign == 0 {
		return int16(-sample)
	}
	return int16(sample)
}

func encodeALawSample(sample int16) byte {
	s := int(sample)
	sign := signBit
	if s < 0 {
		sign = 0
		s = -s - 1
		if s < 0 {
			s = 0
		}
	}
	if s > aLawClip {
		s = aLawClip
	}
	var enc byte
	if s >= aLawLinearThreshold {
		exponent := maxSegmentExponent
		for mask := topSegmentBit; s&mask == 0 && exponent > 0; mask >>= 1 {
			exponent--
		}
		mantissa := (s >> (exponent + mantissaBitShift)) & mantissaMask
		enc = byte((exponent << nibbleShift) | mantissa) //nolint:gosec // result is masked to one byte
	} else {
		enc = byte(s >> nibbleShift)
	}
	return (byte(sign) | enc) ^ aLawToggleMask
}
