package stt

import (
	"context"
)

const (
	// Default audio settings.
	DefaultSampleRate = 16000
	DefaultChannels   = 1
	DefaultBitDepth   = 16

	// Common audio formats.
	FormatPCM = "pcm"
	FormatWAV = "wav"
	FormatMP3 = "mp3"
)

// Service transcribes audio to text.
// This interface abstracts different STT providers (OpenAI Whisper, Google, etc.)
// enabling voice AI applications to use any provider interchangeably.
type Service interface {
	// Name returns the provider identifier (for logging/debugging).
	Name() string

	// Transcribe converts audio to text.
	// Returns the transcribed text or an error if transcription fails.
	Transcribe(ctx context.Context, audio []byte, config TranscriptionConfig) (string, error)

	// SupportedFormats returns supported audio input formats.
	// Common values: "pcm", "wav", "mp3", "m4a", "webm"
	SupportedFormats() []string
}

// TranscriptionConfig configures speech-to-text transcription.
type TranscriptionConfig struct {
	// Format is the audio format ("pcm", "wav", "mp3").
	// Default: "pcm"
	Format string

	// SampleRate is the audio sample rate in Hz.
	// Default: 16000
	SampleRate int

	// Channels is the number of audio channels (1=mono, 2=stereo).
	// Default: 1
	Channels int

	// BitDepth is the bits per sample for PCM audio.
	// Default: 16
	BitDepth int

	// Language is a hint for the transcription language (e.g., "en", "es").
	// Optional - improves accuracy if provided.
	Language string

	// Model is the STT model to use (provider-specific).
	// For OpenAI: "whisper-1"
	Model string

	// Prompt is a text prompt to guide transcription (provider-specific).
	// Can improve accuracy for domain-specific vocabulary.
	Prompt string
}

// DefaultTranscriptionConfig returns sensible defaults for transcription.
func DefaultTranscriptionConfig() TranscriptionConfig {
	return TranscriptionConfig{
		Format:     FormatPCM,
		SampleRate: DefaultSampleRate,
		Channels:   DefaultChannels,
		BitDepth:   DefaultBitDepth,
		Language:   "en",
	}
}
