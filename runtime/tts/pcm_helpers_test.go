//go:build cartesia_integration || elevenlabs_integration

package tts_test

import "math"

// pcm16RMS computes the root-mean-square amplitude of a little-endian
// signed 16-bit PCM buffer. Output is on the [0, 32768] scale; values
// near 0 indicate silence, values >2000 indicate strong signal. Shared
// across the cartesia / elevenlabs integration probes.
func pcm16RMS(b []byte) float64 {
	if len(b) < 2 {
		return 0
	}
	n := len(b) / 2
	var sumSq float64
	for i := 0; i < n; i++ {
		sample := int16(b[2*i]) | int16(b[2*i+1])<<8
		f := float64(sample)
		sumSq += f * f
	}
	return math.Sqrt(sumSq / float64(n))
}

// pcm16Peak returns the absolute maximum sample magnitude of a
// little-endian signed 16-bit PCM buffer. Useful complement to RMS:
// peak tracks instantaneous loudness while RMS averages it.
func pcm16Peak(b []byte) int {
	if len(b) < 2 {
		return 0
	}
	n := len(b) / 2
	peak := 0
	for i := 0; i < n; i++ {
		sample := int(int16(b[2*i]) | int16(b[2*i+1])<<8)
		if sample < 0 {
			sample = -sample
		}
		if sample > peak {
			peak = sample
		}
	}
	return peak
}
