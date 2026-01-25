package media

import (
	"context"
	"testing"
)

func TestNewAudioConverter(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		config := DefaultAudioConverterConfig()
		conv := NewAudioConverter(config)

		if conv == nil {
			t.Fatal("expected non-nil converter")
		}
		if conv.config.FFmpegPath != "ffmpeg" {
			t.Errorf("expected FFmpegPath 'ffmpeg', got %q", conv.config.FFmpegPath)
		}
		if conv.config.FFmpegTimeout != 300 {
			t.Errorf("expected FFmpegTimeout 300, got %d", conv.config.FFmpegTimeout)
		}
	})

	t.Run("empty config uses defaults", func(t *testing.T) {
		conv := NewAudioConverter(AudioConverterConfig{})

		if conv.config.FFmpegPath != "ffmpeg" {
			t.Errorf("expected FFmpegPath 'ffmpeg', got %q", conv.config.FFmpegPath)
		}
		if conv.config.FFmpegTimeout != 300 {
			t.Errorf("expected FFmpegTimeout 300, got %d", conv.config.FFmpegTimeout)
		}
	})

	t.Run("custom config", func(t *testing.T) {
		config := AudioConverterConfig{
			FFmpegPath:    "/usr/local/bin/ffmpeg",
			FFmpegTimeout: 60,
			SampleRate:    16000,
			Channels:      1,
			BitRate:       "128k",
		}
		conv := NewAudioConverter(config)

		if conv.config.FFmpegPath != "/usr/local/bin/ffmpeg" {
			t.Errorf("expected custom FFmpegPath, got %q", conv.config.FFmpegPath)
		}
		if conv.config.FFmpegTimeout != 60 {
			t.Errorf("expected FFmpegTimeout 60, got %d", conv.config.FFmpegTimeout)
		}
		if conv.config.SampleRate != 16000 {
			t.Errorf("expected SampleRate 16000, got %d", conv.config.SampleRate)
		}
	})
}

func TestAudioConverter_ConvertAudio_SameFormat(t *testing.T) {
	conv := NewAudioConverter(DefaultAudioConverterConfig())
	ctx := context.Background()

	// Same format should return original data without conversion
	data := []byte("test audio data")
	result, err := conv.ConvertAudio(ctx, data, MIMETypeAudioWAV, MIMETypeAudioWAV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.WasConverted {
		t.Error("expected WasConverted to be false for same format")
	}
	if string(result.Data) != string(data) {
		t.Error("expected data to be unchanged")
	}
	if result.Format != AudioFormatWAV {
		t.Errorf("expected format %q, got %q", AudioFormatWAV, result.Format)
	}
	if result.MIMEType != MIMETypeAudioWAV {
		t.Errorf("expected MIME type %q, got %q", MIMETypeAudioWAV, result.MIMEType)
	}
}

func TestAudioConverter_ConvertAudio_EmptyData(t *testing.T) {
	conv := NewAudioConverter(DefaultAudioConverterConfig())
	ctx := context.Background()

	_, err := conv.ConvertAudio(ctx, []byte{}, MIMETypeAudioWAV, MIMETypeAudioMP3)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestAudioConverter_CanConvert(t *testing.T) {
	conv := NewAudioConverter(DefaultAudioConverterConfig())

	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		{"wav to mp3", MIMETypeAudioWAV, MIMETypeAudioMP3, true},
		{"mp3 to wav", MIMETypeAudioMP3, MIMETypeAudioWAV, true},
		{"ogg to flac", MIMETypeAudioOGG, MIMETypeAudioFLAC, true},
		{"unknown to wav", "audio/unknown", MIMETypeAudioWAV, false},
		{"wav to unknown", MIMETypeAudioWAV, "audio/unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conv.CanConvert(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("CanConvert(%q, %q) = %v, expected %v", tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestIsFormatSupported(t *testing.T) {
	tests := []struct {
		name      string
		mimeType  string
		supported []string
		expected  bool
	}{
		{
			name:      "exact match",
			mimeType:  MIMETypeAudioWAV,
			supported: []string{MIMETypeAudioWAV, MIMETypeAudioMP3},
			expected:  true,
		},
		{
			name:      "normalized match",
			mimeType:  "audio/x-wav",
			supported: []string{MIMETypeAudioWAV},
			expected:  true,
		},
		{
			name:      "no match",
			mimeType:  MIMETypeAudioFLAC,
			supported: []string{MIMETypeAudioWAV, MIMETypeAudioMP3},
			expected:  false,
		},
		{
			name:      "empty supported list",
			mimeType:  MIMETypeAudioWAV,
			supported: []string{},
			expected:  false,
		},
		{
			name:      "with mime parameters",
			mimeType:  "audio/wav; codecs=1",
			supported: []string{MIMETypeAudioWAV},
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFormatSupported(tt.mimeType, tt.supported)
			if result != tt.expected {
				t.Errorf("IsFormatSupported(%q, %v) = %v, expected %v", tt.mimeType, tt.supported, result, tt.expected)
			}
		})
	}
}

func TestSelectTargetFormat(t *testing.T) {
	tests := []struct {
		name      string
		supported []string
		expected  string
	}{
		{
			name:      "prefers wav",
			supported: []string{MIMETypeAudioMP3, MIMETypeAudioWAV, MIMETypeAudioOGG},
			expected:  MIMETypeAudioWAV,
		},
		{
			name:      "falls back to mp3",
			supported: []string{MIMETypeAudioMP3, MIMETypeAudioOGG},
			expected:  MIMETypeAudioMP3,
		},
		{
			name:      "uses first if no preference matches",
			supported: []string{MIMETypeAudioFLAC, MIMETypeAudioOGG},
			expected:  MIMETypeAudioFLAC,
		},
		{
			name:      "empty returns default",
			supported: []string{},
			expected:  MIMETypeAudioWAV,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectTargetFormat(tt.supported)
			if result != tt.expected {
				t.Errorf("SelectTargetFormat(%v) = %q, expected %q", tt.supported, result, tt.expected)
			}
		})
	}
}

func TestMIMETypeToAudioFormat(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{MIMETypeAudioWAV, AudioFormatWAV},
		{"audio/x-wav", AudioFormatWAV},
		{MIMETypeAudioMP3, AudioFormatMP3},
		{MIMETypeAudioFLAC, AudioFormatFLAC},
		{MIMETypeAudioOGG, AudioFormatOGG},
		{MIMETypeAudioM4A, AudioFormatM4A},
		{MIMETypeAudioAAC, AudioFormatAAC},
		{MIMETypeAudioPCM, AudioFormatPCM},
		{"audio/L16", AudioFormatPCM},
		{MIMETypeAudioWebM, AudioFormatWebM},
		{"unknown", AudioFormatWAV}, // default
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := MIMETypeToAudioFormat(tt.mimeType)
			if result != tt.expected {
				t.Errorf("MIMETypeToAudioFormat(%q) = %q, expected %q", tt.mimeType, result, tt.expected)
			}
		})
	}
}

func TestAudioFormatToMIMEType(t *testing.T) {
	tests := []struct {
		format   string
		expected string
	}{
		{AudioFormatWAV, MIMETypeAudioWAV},
		{AudioFormatMP3, MIMETypeAudioMP3},
		{AudioFormatFLAC, MIMETypeAudioFLAC},
		{AudioFormatOGG, MIMETypeAudioOGG},
		{AudioFormatM4A, MIMETypeAudioM4A},
		{AudioFormatAAC, MIMETypeAudioAAC},
		{AudioFormatPCM, MIMETypeAudioPCM},
		{AudioFormatWebM, MIMETypeAudioWebM},
		{"unknown", MIMETypeAudioWAV}, // default
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			result := AudioFormatToMIMEType(tt.format)
			if result != tt.expected {
				t.Errorf("AudioFormatToMIMEType(%q) = %q, expected %q", tt.format, result, tt.expected)
			}
		})
	}
}

func TestNormalizeMIMEType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"audio/wav", MIMETypeAudioWAV},
		{"audio/x-wav", MIMETypeAudioWAV},
		{"audio/wave", MIMETypeAudioWAV},
		{"audio/mp3", MIMETypeAudioMP3},
		{"audio/x-flac", MIMETypeAudioFLAC},
		{"audio/x-m4a", MIMETypeAudioM4A},
		{"audio/mpeg", MIMETypeAudioMP3},
		{"AUDIO/MPEG", MIMETypeAudioMP3}, // case insensitive
		{"audio/wav; codecs=1", MIMETypeAudioWAV}, // strips parameters
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeMIMEType(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeMIMEType(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultAudioConverterConfig(t *testing.T) {
	config := DefaultAudioConverterConfig()

	if config.FFmpegPath != "ffmpeg" {
		t.Errorf("expected default FFmpegPath 'ffmpeg', got %q", config.FFmpegPath)
	}
	if config.FFmpegTimeout != 300 {
		t.Errorf("expected default FFmpegTimeout 300, got %d", config.FFmpegTimeout)
	}
	if config.SampleRate != 0 {
		t.Errorf("expected default SampleRate 0, got %d", config.SampleRate)
	}
	if config.Channels != 0 {
		t.Errorf("expected default Channels 0, got %d", config.Channels)
	}
	if config.BitRate != "" {
		t.Errorf("expected default BitRate '', got %q", config.BitRate)
	}
}

func TestAudioConvertResult(t *testing.T) {
	result := AudioConvertResult{
		Data:         []byte("converted"),
		Format:       AudioFormatWAV,
		MIMEType:     MIMETypeAudioWAV,
		OriginalSize: 100,
		NewSize:      80,
		WasConverted: true,
	}

	if string(result.Data) != "converted" {
		t.Error("data mismatch")
	}
	if result.Format != AudioFormatWAV {
		t.Error("format mismatch")
	}
	if result.MIMEType != MIMETypeAudioWAV {
		t.Error("MIME type mismatch")
	}
	if result.OriginalSize != 100 {
		t.Error("original size mismatch")
	}
	if result.NewSize != 80 {
		t.Error("new size mismatch")
	}
	if !result.WasConverted {
		t.Error("WasConverted should be true")
	}
}

func TestCheckFFmpegAvailable(t *testing.T) {
	t.Run("empty path uses default", func(t *testing.T) {
		// This test might pass or fail depending on whether ffmpeg is installed
		// Just ensure it doesn't panic
		_ = CheckFFmpegAvailable("")
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		err := CheckFFmpegAvailable("/nonexistent/path/to/ffmpeg")
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})
}

func TestAudioConverter_ConvertAudio_NormalizedMIME(t *testing.T) {
	conv := NewAudioConverter(DefaultAudioConverterConfig())
	ctx := context.Background()

	// Test with non-normalized MIME types that should match after normalization
	data := []byte("test audio data")

	// audio/x-wav should normalize to audio/wav
	result, err := conv.ConvertAudio(ctx, data, "audio/x-wav", MIMETypeAudioWAV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WasConverted {
		t.Error("normalized same format should not trigger conversion")
	}

	// audio/wave should normalize to audio/wav
	result2, err := conv.ConvertAudio(ctx, data, "audio/wave", MIMETypeAudioWAV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.WasConverted {
		t.Error("normalized same format should not trigger conversion")
	}
}

func TestIsFormatSupported_NormalizedComparison(t *testing.T) {
	// Test that normalization works in format comparison
	supported := []string{MIMETypeAudioWAV}

	// audio/x-wav should match audio/wav
	if !IsFormatSupported("audio/x-wav", supported) {
		t.Error("audio/x-wav should be supported when audio/wav is in list")
	}

	// audio/wave should match audio/wav
	if !IsFormatSupported("audio/wave", supported) {
		t.Error("audio/wave should be supported when audio/wav is in list")
	}

	// With parameters should still match
	if !IsFormatSupported("audio/wav; codecs=1", supported) {
		t.Error("audio/wav with params should be supported")
	}
}

func TestSelectTargetFormat_EdgeCases(t *testing.T) {
	t.Run("single format", func(t *testing.T) {
		result := SelectTargetFormat([]string{MIMETypeAudioOGG})
		if result != MIMETypeAudioOGG {
			t.Errorf("expected %q, got %q", MIMETypeAudioOGG, result)
		}
	})

	t.Run("prefers wav over others", func(t *testing.T) {
		// WAV should be selected even if listed last
		result := SelectTargetFormat([]string{MIMETypeAudioOGG, MIMETypeAudioFLAC, MIMETypeAudioWAV})
		if result != MIMETypeAudioWAV {
			t.Errorf("expected WAV to be preferred, got %q", result)
		}
	})

	t.Run("prefers mp3 when no wav", func(t *testing.T) {
		result := SelectTargetFormat([]string{MIMETypeAudioOGG, MIMETypeAudioMP3, MIMETypeAudioFLAC})
		if result != MIMETypeAudioMP3 {
			t.Errorf("expected MP3 to be preferred when no WAV, got %q", result)
		}
	})
}
