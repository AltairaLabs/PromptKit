// Package media provides utilities for processing media content.
package media

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Audio format constants.
const (
	AudioFormatWAV  = "wav"
	AudioFormatMP3  = "mp3"
	AudioFormatFLAC = "flac"
	AudioFormatOGG  = "ogg"
	AudioFormatM4A  = "m4a"
	AudioFormatAAC  = "aac"
	AudioFormatPCM  = "pcm"
	AudioFormatWebM = "webm"
)

// Audio MIME type constants.
const (
	MIMETypeAudioWAV  = "audio/wav"
	MIMETypeAudioMP3  = "audio/mpeg"
	MIMETypeAudioFLAC = "audio/flac"
	MIMETypeAudioOGG  = "audio/ogg"
	MIMETypeAudioM4A  = "audio/mp4"
	MIMETypeAudioAAC  = "audio/aac"
	MIMETypeAudioPCM  = "audio/pcm"
	MIMETypeAudioWebM = "audio/webm"
)

// MIME type variant constants.
const (
	MIMETypeAudioXWAV = "audio/x-wav"
)

// Default configuration values.
const (
	DefaultFFmpegPath          = "ffmpeg"
	DefaultFFmpegTimeout       = 300  // 5 minutes
	DefaultFFmpegCheckTimeout  = 5    // seconds for availability check
	DefaultTempFilePermissions = 0600 // owner read/write only
)

// AudioConverterConfig configures audio conversion behavior.
type AudioConverterConfig struct {
	// FFmpegPath is the path to the ffmpeg binary.
	// Default: "ffmpeg" (uses PATH).
	FFmpegPath string

	// FFmpegTimeout is the maximum time for FFmpeg execution.
	// Default: 5 minutes.
	FFmpegTimeout int // seconds

	// SampleRate is the output sample rate in Hz.
	// 0 means preserve original.
	SampleRate int

	// Channels is the number of output channels.
	// 0 means preserve original.
	Channels int

	// BitRate is the output bitrate for lossy formats (e.g., "128k").
	// Empty means use ffmpeg default.
	BitRate string
}

// DefaultAudioConverterConfig returns sensible defaults for audio conversion.
func DefaultAudioConverterConfig() AudioConverterConfig {
	return AudioConverterConfig{
		FFmpegPath:    DefaultFFmpegPath,
		FFmpegTimeout: DefaultFFmpegTimeout,
		SampleRate:    0,  // Preserve original
		Channels:      0,  // Preserve original
		BitRate:       "", // Use ffmpeg default
	}
}

// AudioConvertResult contains the result of an audio conversion.
type AudioConvertResult struct {
	Data         []byte
	Format       string
	MIMEType     string
	OriginalSize int64
	NewSize      int64
	WasConverted bool
}

// AudioConverter handles audio format conversion using ffmpeg.
type AudioConverter struct {
	config AudioConverterConfig
}

// NewAudioConverter creates a new audio converter with the given config.
func NewAudioConverter(config AudioConverterConfig) *AudioConverter {
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}
	if config.FFmpegTimeout <= 0 {
		config.FFmpegTimeout = 300
	}
	return &AudioConverter{config: config}
}

// ConvertAudio converts audio data from one format to another.
// If the source format matches the target, returns the original data unchanged.
func (c *AudioConverter) ConvertAudio(
	ctx context.Context,
	data []byte,
	fromMIME, toMIME string,
) (*AudioConvertResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty audio data")
	}

	// Normalize MIME types
	fromMIME = normalizeMIMEType(fromMIME)
	toMIME = normalizeMIMEType(toMIME)

	// Check if conversion is needed
	if fromMIME == toMIME {
		return &AudioConvertResult{
			Data:         data,
			Format:       MIMETypeToAudioFormat(toMIME),
			MIMEType:     toMIME,
			OriginalSize: int64(len(data)),
			NewSize:      int64(len(data)),
			WasConverted: false,
		}, nil
	}

	// Perform conversion using ffmpeg
	converted, err := c.convertWithFFmpeg(ctx, data, fromMIME, toMIME)
	if err != nil {
		return nil, fmt.Errorf("audio conversion failed: %w", err)
	}

	return &AudioConvertResult{
		Data:         converted,
		Format:       MIMETypeToAudioFormat(toMIME),
		MIMEType:     toMIME,
		OriginalSize: int64(len(data)),
		NewSize:      int64(len(converted)),
		WasConverted: true,
	}, nil
}

// CanConvert checks if the converter can convert between the given formats.
func (c *AudioConverter) CanConvert(fromMIME, toMIME string) bool {
	fromMIME = normalizeMIMEType(fromMIME)
	toMIME = normalizeMIMEType(toMIME)

	// Check if formats are supported
	supportedFormats := map[string]bool{
		MIMETypeAudioWAV:  true,
		MIMETypeAudioMP3:  true,
		MIMETypeAudioFLAC: true,
		MIMETypeAudioOGG:  true,
		MIMETypeAudioM4A:  true,
		MIMETypeAudioAAC:  true,
		MIMETypeAudioPCM:  true,
	}

	return supportedFormats[fromMIME] && supportedFormats[toMIME]
}

// IsFormatSupported checks if a MIME type is in the list of supported formats.
func IsFormatSupported(mimeType string, supportedFormats []string) bool {
	mimeType = normalizeMIMEType(mimeType)
	for _, supported := range supportedFormats {
		if normalizeMIMEType(supported) == mimeType {
			return true
		}
	}
	return false
}

// SelectTargetFormat selects the best target format from supported formats.
// Prefers lossless formats (WAV) when available, then common formats (MP3).
func SelectTargetFormat(supportedFormats []string) string {
	if len(supportedFormats) == 0 {
		return MIMETypeAudioWAV // Default fallback
	}

	// Preference order: WAV (lossless), then MP3 (widely supported)
	preferences := []string{
		MIMETypeAudioWAV,
		types.MIMETypeAudioWAV, // Handle types package constant
		MIMETypeAudioMP3,
		types.MIMETypeAudioMP3,
	}

	for _, pref := range preferences {
		if IsFormatSupported(pref, supportedFormats) {
			return normalizeMIMEType(pref)
		}
	}

	// Return first supported format
	return normalizeMIMEType(supportedFormats[0])
}

// MIMETypeToAudioFormat converts a MIME type to a format string.
func MIMETypeToAudioFormat(mimeType string) string {
	mimeType = normalizeMIMEType(mimeType)
	switch mimeType {
	case MIMETypeAudioWAV, MIMETypeAudioXWAV:
		return AudioFormatWAV
	case MIMETypeAudioMP3:
		return AudioFormatMP3
	case MIMETypeAudioFLAC:
		return AudioFormatFLAC
	case MIMETypeAudioOGG:
		return AudioFormatOGG
	case MIMETypeAudioM4A:
		return AudioFormatM4A
	case MIMETypeAudioAAC:
		return AudioFormatAAC
	case MIMETypeAudioPCM, "audio/L16", "audio/l16":
		return AudioFormatPCM
	case MIMETypeAudioWebM:
		return AudioFormatWebM
	default:
		return AudioFormatWAV
	}
}

// AudioFormatToMIMEType converts a format string to MIME type.
func AudioFormatToMIMEType(format string) string {
	switch strings.ToLower(format) {
	case AudioFormatWAV:
		return MIMETypeAudioWAV
	case AudioFormatMP3:
		return MIMETypeAudioMP3
	case AudioFormatFLAC:
		return MIMETypeAudioFLAC
	case AudioFormatOGG:
		return MIMETypeAudioOGG
	case AudioFormatM4A:
		return MIMETypeAudioM4A
	case AudioFormatAAC:
		return MIMETypeAudioAAC
	case AudioFormatPCM:
		return MIMETypeAudioPCM
	case AudioFormatWebM:
		return MIMETypeAudioWebM
	default:
		return MIMETypeAudioWAV
	}
}

// normalizeMIMEType normalizes MIME type variations to a canonical form.
func normalizeMIMEType(mimeType string) string {
	// Remove any parameters (e.g., "audio/wav; codecs=...")
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	mimeType = strings.ToLower(mimeType)

	// Normalize common variations
	switch mimeType {
	case "audio/x-wav", "audio/wave":
		return MIMETypeAudioWAV
	case "audio/mp3":
		return MIMETypeAudioMP3
	case "audio/x-flac":
		return MIMETypeAudioFLAC
	case "audio/x-m4a":
		return MIMETypeAudioM4A
	default:
		return mimeType
	}
}

// buildFFmpegArgs constructs ffmpeg command arguments for converting an input
// file to toFormat. It is pure — it derives the argument slice from the
// converter config and target format without touching the filesystem or
// invoking ffmpeg — so it is unit-tested directly (the actual ffmpeg exec lives
// in audio_converter_integration.go).
func (c *AudioConverter) buildFFmpegArgs(inputPath, outputPath, toFormat string) []string {
	args := []string{
		"-y",
		"-i", inputPath,
		"-vn",
	}

	// Add sample rate if specified
	if c.config.SampleRate > 0 {
		args = append(args, "-ar", fmt.Sprintf("%d", c.config.SampleRate))
	}

	// Add channels if specified
	if c.config.Channels > 0 {
		args = append(args, "-ac", fmt.Sprintf("%d", c.config.Channels))
	}

	// Format-specific options
	switch toFormat {
	case AudioFormatWAV:
		// PCM 16-bit signed little-endian for maximum compatibility
		args = append(args, "-acodec", "pcm_s16le")

	case AudioFormatMP3:
		args = append(args, "-acodec", "libmp3lame")
		if c.config.BitRate != "" {
			args = append(args, "-b:a", c.config.BitRate)
		} else {
			args = append(args, "-b:a", "192k") // Default to good quality
		}

	case AudioFormatFLAC:
		args = append(args, "-acodec", "flac")

	case AudioFormatOGG:
		args = append(args, "-acodec", "libvorbis")
		if c.config.BitRate != "" {
			args = append(args, "-b:a", c.config.BitRate)
		}

	case AudioFormatAAC, AudioFormatM4A:
		args = append(args, "-acodec", "aac")
		if c.config.BitRate != "" {
			args = append(args, "-b:a", c.config.BitRate)
		}
	}

	// Output file
	args = append(args, outputPath)

	return args
}
