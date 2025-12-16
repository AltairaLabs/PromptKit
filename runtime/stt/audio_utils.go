package stt

// WAV header constants.
const (
	wavHeaderSize = 44
)

// WrapPCMAsWAV wraps raw PCM audio data in a WAV header.
// This is necessary for APIs like OpenAI Whisper that expect file uploads.
//
// Parameters:
//   - pcmData: Raw PCM audio bytes (little-endian, signed)
//   - sampleRate: Sample rate in Hz (e.g., 16000)
//   - channels: Number of channels (1=mono, 2=stereo)
//   - bitsPerSample: Bits per sample (typically 16)
//
// Returns a byte slice containing WAV-formatted audio.
func WrapPCMAsWAV(pcmData []byte, sampleRate, channels, bitsPerSample int) []byte {
	dataSize := len(pcmData)
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	// WAV header is 44 bytes
	wav := make([]byte, wavHeaderSize+dataSize)

	// RIFF header
	copy(wav[0:4], "RIFF")
	putLE32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")

	// fmt subchunk
	copy(wav[12:16], "fmt ")
	putLE32(wav[16:20], 16) // Subchunk1Size for PCM
	putLE16(wav[20:22], 1)  // AudioFormat (1 = PCM)
	putLE16(wav[22:24], uint16(channels))
	putLE32(wav[24:28], uint32(sampleRate))
	putLE32(wav[28:32], uint32(byteRate))
	putLE16(wav[32:34], uint16(blockAlign))
	putLE16(wav[34:36], uint16(bitsPerSample))

	// data subchunk
	copy(wav[36:40], "data")
	putLE32(wav[40:44], uint32(dataSize))
	copy(wav[44:], pcmData)

	return wav
}

// putLE16 writes a uint16 in little-endian format.
func putLE16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

// putLE32 writes a uint32 in little-endian format.
func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
