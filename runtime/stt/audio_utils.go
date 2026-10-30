package stt

import "encoding/binary"

// WAV header constants.
const (
	wavHeaderSize = 44
	// riffSizeOverhead is the header byte count counted in the RIFF chunk size
	// (the whole header minus the "RIFF" tag and the size field itself).
	riffSizeOverhead = 36
	// pcmFmtChunkSize is the size of the "fmt " subchunk for PCM audio.
	pcmFmtChunkSize = 16
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
	byteRate := sampleRate * channels * bitsPerSample / bitsPerByte
	blockAlign := channels * bitsPerSample / bitsPerByte

	// WAV header is 44 bytes
	wav := make([]byte, wavHeaderSize+dataSize)

	// RIFF header
	copy(wav[0:4], "RIFF")
	putLE32(wav[4:8], wavU32(riffSizeOverhead+dataSize))
	copy(wav[8:12], "WAVE")

	// fmt subchunk
	copy(wav[12:16], "fmt ")
	putLE32(wav[16:20], pcmFmtChunkSize) // Subchunk1Size for PCM
	putLE16(wav[20:22], 1)               // AudioFormat (1 = PCM)
	putLE16(wav[22:24], wavU16(channels))
	putLE32(wav[24:28], wavU32(sampleRate))
	putLE32(wav[28:32], wavU32(byteRate))
	putLE16(wav[32:34], wavU16(blockAlign))
	putLE16(wav[34:36], wavU16(bitsPerSample))

	// data subchunk
	copy(wav[36:40], "data")
	putLE32(wav[40:44], wavU32(dataSize))
	copy(wav[44:], pcmData)

	return wav
}

// wavU32 converts an int to the uint32 a WAV header field holds.
func wavU32(v int) uint32 {
	return uint32(v) //nolint:gosec // G115: WAV header fields are 32-bit by format; wraparound matches the prior behavior
}

// wavU16 converts an int to the uint16 a WAV header field holds.
func wavU16(v int) uint16 {
	return uint16(v) //nolint:gosec // G115: WAV header fields are 16-bit by format; wraparound matches the prior behavior
}

// putLE16 writes a uint16 in little-endian format.
func putLE16(b []byte, v uint16) {
	binary.LittleEndian.PutUint16(b, v)
}

// putLE32 writes a uint32 in little-endian format.
func putLE32(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b, v)
}
