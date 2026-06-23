package voice

import (
	"encoding/binary"
	"math"
)

const (
	// pcm16BytesPerSample is the number of bytes in one PCM16 sample.
	pcm16BytesPerSample = 2
	// pcm16MaxAmplitude is the maximum absolute value of a signed 16-bit sample,
	// used to normalize RMS to the 0..1 range.
	pcm16MaxAmplitude = 32768.0
)

// rms returns the normalized (0..1) root-mean-square level of PCM16 mono bytes.
func rms(frame []byte) float32 {
	n := len(frame) / pcm16BytesPerSample
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		raw := binary.LittleEndian.Uint16(frame[i*pcm16BytesPerSample:])
		s := float64(int16(raw)) //nolint:gosec // PCM16 reinterpretation, not a numeric conversion
		sum += s * s
	}
	return float32(math.Sqrt(sum/float64(n)) / pcm16MaxAmplitude)
}
