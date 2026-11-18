package audio

import "encoding/binary"

// FadeOutPCM16 returns a copy of the first n samples of little-endian mono
// PCM16 audio (all of them when there are fewer), scaled linearly from full
// level down toward silence. A player that stops on barge-in by dropping its
// queued audio cuts the speaker off mid-waveform, which is heard as an abrupt
// stop and often a click; playing this ramp first and dropping the rest ends
// it softly. An odd trailing byte is not a sample and is left out.
func FadeOutPCM16(pcm []byte, n int) []byte {
	if avail := len(pcm) / bytesPerSample; n > avail {
		n = avail
	}
	if n <= 0 {
		return nil
	}
	out := make([]byte, bytesPerSample*n)
	for i := 0; i < n; i++ {
		//nolint:gosec // reinterpreting PCM16 wire bytes as a signed sample; same bit pattern
		s := int16(binary.LittleEndian.Uint16(pcm[bytesPerSample*i:]))
		//nolint:gosec // a gain of at most 1 keeps the sample within int16; same bit pattern on the wire
		binary.LittleEndian.PutUint16(out[bytesPerSample*i:], uint16(rampSample(s, i, n)))
	}
	return out
}

// rampSample is sample s, the i-th of n in a fade-out, at gain (n-i)/n.
func rampSample(s int16, i, n int) int16 {
	return int16(int(s) * (n - i) / n) //nolint:gosec // a gain of at most 1 keeps the sample within int16
}
