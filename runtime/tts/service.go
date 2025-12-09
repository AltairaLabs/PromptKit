package tts

import (
	"context"
	"io"
)

// Common audio constants.
const (
	// Standard sample rates.
	sampleRateDefault = 24000

	// Bit depths.
	bitDepthDefault = 16
)

// Service converts text to speech audio.
// This interface abstracts different TTS providers (OpenAI, ElevenLabs, etc.)
// enabling voice AI applications to use any provider interchangeably.
type Service interface {
	// Name returns the provider identifier (for logging/debugging).
	Name() string

	// Synthesize converts text to audio.
	// Returns a reader for streaming audio data.
	// The caller is responsible for closing the reader.
	Synthesize(ctx context.Context, text string, config SynthesisConfig) (io.ReadCloser, error)

	// SupportedVoices returns available voices for this provider.
	SupportedVoices() []Voice

	// SupportedFormats returns supported audio output formats.
	SupportedFormats() []AudioFormat
}

// StreamingService extends Service with streaming synthesis capabilities.
// Streaming TTS provides lower latency by returning audio chunks as they're generated.
type StreamingService interface {
	Service

	// SynthesizeStream converts text to audio with streaming output.
	// Returns a channel that receives audio chunks as they're generated.
	// The channel is closed when synthesis completes or an error occurs.
	SynthesizeStream(ctx context.Context, text string, config SynthesisConfig) (<-chan AudioChunk, error)
}

// AudioChunk represents a chunk of synthesized audio data.
type AudioChunk struct {
	// Data is the raw audio bytes.
	Data []byte

	// Index is the chunk sequence number (0-indexed).
	Index int

	// Final indicates this is the last chunk.
	Final bool

	// Error is set if an error occurred during synthesis.
	Error error
}

// SynthesisConfig configures text-to-speech synthesis.
type SynthesisConfig struct {
	// Voice is the voice ID to use for synthesis.
	// Available voices vary by provider - use SupportedVoices() to list options.
	Voice string

	// Format is the output audio format.
	// Default is MP3 for most providers.
	Format AudioFormat

	// Speed is the speech rate multiplier (0.25-4.0, default 1.0).
	// Not all providers support speed adjustment.
	Speed float64

	// Pitch adjusts the voice pitch (-20 to 20 semitones, default 0).
	// Not all providers support pitch adjustment.
	Pitch float64

	// Language is the language code for synthesis (e.g., "en-US").
	// Required for some providers, optional for others.
	Language string

	// Model is the TTS model to use (provider-specific).
	// For OpenAI: "tts-1" (fast) or "tts-1-hd" (high quality).
	Model string
}

// DefaultSynthesisConfig returns sensible defaults for synthesis.
func DefaultSynthesisConfig() SynthesisConfig {
	return SynthesisConfig{
		Voice:  "alloy", // OpenAI default
		Format: FormatMP3,
		Speed:  1.0,
		Pitch:  0,
	}
}

// Voice describes a TTS voice available from a provider.
type Voice struct {
	// ID is the provider-specific voice identifier.
	ID string

	// Name is a human-readable voice name.
	Name string

	// Language is the primary language code (e.g., "en", "es", "fr").
	Language string

	// Gender is the voice gender ("male", "female", "neutral").
	Gender string

	// Description provides additional voice characteristics.
	Description string

	// Preview is a URL to a voice sample (if available).
	Preview string
}

// AudioFormat describes an audio output format.
type AudioFormat struct {
	// Name is the format identifier ("mp3", "opus", "pcm", "aac", "flac").
	Name string

	// MIMEType is the content type (e.g., "audio/mpeg").
	MIMEType string

	// SampleRate is the audio sample rate in Hz.
	SampleRate int

	// BitDepth is the bits per sample (for PCM formats).
	BitDepth int

	// Channels is the number of audio channels (1=mono, 2=stereo).
	Channels int
}

// Common audio formats.
var (
	// FormatMP3 is MP3 format (most compatible).
	FormatMP3 = AudioFormat{
		Name:       "mp3",
		MIMEType:   "audio/mpeg",
		SampleRate: sampleRateDefault,
		BitDepth:   0, // Compressed
		Channels:   1,
	}

	// FormatOpus is Opus format (best for streaming).
	FormatOpus = AudioFormat{
		Name:       "opus",
		MIMEType:   "audio/opus",
		SampleRate: sampleRateDefault,
		BitDepth:   0, // Compressed
		Channels:   1,
	}

	// FormatAAC is AAC format.
	FormatAAC = AudioFormat{
		Name:       "aac",
		MIMEType:   "audio/aac",
		SampleRate: sampleRateDefault,
		BitDepth:   0, // Compressed
		Channels:   1,
	}

	// FormatFLAC is FLAC format (lossless).
	FormatFLAC = AudioFormat{
		Name:       "flac",
		MIMEType:   "audio/flac",
		SampleRate: sampleRateDefault,
		BitDepth:   bitDepthDefault,
		Channels:   1,
	}

	// FormatPCM16 is raw 16-bit PCM (for processing).
	FormatPCM16 = AudioFormat{
		Name:       "pcm",
		MIMEType:   "audio/pcm",
		SampleRate: sampleRateDefault,
		BitDepth:   bitDepthDefault,
		Channels:   1,
	}

	// FormatWAV is WAV format (PCM with header).
	FormatWAV = AudioFormat{
		Name:       "wav",
		MIMEType:   "audio/wav",
		SampleRate: sampleRateDefault,
		BitDepth:   bitDepthDefault,
		Channels:   1,
	}
)

// String returns the format name.
func (f AudioFormat) String() string {
	return f.Name
}
