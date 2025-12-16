package tts

import (
	"testing"
)

func TestAudioFormat_String(t *testing.T) {
	tests := []struct {
		format AudioFormat
		want   string
	}{
		{FormatMP3, "mp3"},
		{FormatOpus, "opus"},
		{FormatAAC, "aac"},
		{FormatFLAC, "flac"},
		{FormatPCM16, "pcm"},
		{FormatWAV, "wav"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.format.String(); got != tt.want {
				t.Errorf("AudioFormat.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultSynthesisConfig(t *testing.T) {
	config := DefaultSynthesisConfig()

	if config.Voice != "alloy" {
		t.Errorf("DefaultSynthesisConfig().Voice = %v, want alloy", config.Voice)
	}

	if config.Format.Name != "mp3" {
		t.Errorf("DefaultSynthesisConfig().Format = %v, want mp3", config.Format.Name)
	}

	if config.Speed != 1.0 {
		t.Errorf("DefaultSynthesisConfig().Speed = %v, want 1.0", config.Speed)
	}

	if config.Pitch != 0 {
		t.Errorf("DefaultSynthesisConfig().Pitch = %v, want 0", config.Pitch)
	}
}

func TestVoice(t *testing.T) {
	voice := Voice{
		ID:          "test-id",
		Name:        "Test Voice",
		Language:    "en",
		Gender:      "neutral",
		Description: "A test voice",
		Preview:     "https://example.com/preview.mp3",
	}

	if voice.ID != "test-id" {
		t.Errorf("Voice.ID = %v, want test-id", voice.ID)
	}

	if voice.Name != "Test Voice" {
		t.Errorf("Voice.Name = %v, want Test Voice", voice.Name)
	}
}

func TestAudioFormat_Fields(t *testing.T) {
	format := AudioFormat{
		Name:       "custom",
		MIMEType:   "audio/custom",
		SampleRate: 48000,
		BitDepth:   24,
		Channels:   2,
	}

	if format.SampleRate != 48000 {
		t.Errorf("AudioFormat.SampleRate = %v, want 48000", format.SampleRate)
	}

	if format.BitDepth != 24 {
		t.Errorf("AudioFormat.BitDepth = %v, want 24", format.BitDepth)
	}

	if format.Channels != 2 {
		t.Errorf("AudioFormat.Channels = %v, want 2", format.Channels)
	}
}

func TestAudioChunk(t *testing.T) {
	chunk := AudioChunk{
		Data:  []byte{0x00, 0x01, 0x02},
		Index: 5,
		Final: true,
		Error: nil,
	}

	if len(chunk.Data) != 3 {
		t.Errorf("AudioChunk.Data length = %v, want 3", len(chunk.Data))
	}

	if chunk.Index != 5 {
		t.Errorf("AudioChunk.Index = %v, want 5", chunk.Index)
	}

	if !chunk.Final {
		t.Error("AudioChunk.Final = false, want true")
	}
}
