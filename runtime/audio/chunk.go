package audio

// Chunk represents a chunk of audio data flowing through the runtime —
// produced by TTS providers, consumed by playback sinks, realtime LLM inputs,
// and pipeline stages. Carries the bytes plus stream-position metadata.
type Chunk struct {
	// Data is the raw audio bytes.
	Data []byte
	// Index is the chunk sequence number (0-indexed).
	Index int
	// Final indicates this is the last chunk.
	Final bool
	// Error is set if an error occurred while producing the chunk.
	Error error
}
