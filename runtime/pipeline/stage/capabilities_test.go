package stage

import (
	"testing"
)

func TestContentType_String(t *testing.T) {
	tests := []struct {
		ct   ContentType
		want string
	}{
		{ContentTypeAny, "any"},
		{ContentTypeText, "text"},
		{ContentTypeAudio, "audio"},
		{ContentTypeVideo, "video"},
		{ContentTypeImage, "image"},
		{ContentTypeMessage, "message"},
		{ContentTypeToolCall, "tool_call"},
		{ContentType(999), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.ct.String(); got != tt.want {
			t.Errorf("ContentType(%d).String() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestAudioCapability_AcceptsFormat(t *testing.T) {
	tests := []struct {
		name    string
		cap     *AudioCapability
		format  AudioFormat
		accepts bool
	}{
		{"nil capability accepts any", nil, AudioFormatPCM16, true},
		{"empty formats accepts any", &AudioCapability{}, AudioFormatPCM16, true},
		{"matching format", &AudioCapability{Formats: []AudioFormat{AudioFormatPCM16}}, AudioFormatPCM16, true},
		{"non-matching format", &AudioCapability{Formats: []AudioFormat{AudioFormatPCM16}}, AudioFormatOpus, false},
		{"multiple formats - match", &AudioCapability{Formats: []AudioFormat{AudioFormatPCM16, AudioFormatOpus}}, AudioFormatOpus, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.AcceptsFormat(tt.format); got != tt.accepts {
				t.Errorf("AcceptsFormat() = %v, want %v", got, tt.accepts)
			}
		})
	}
}

func TestAudioCapability_AcceptsSampleRate(t *testing.T) {
	tests := []struct {
		name    string
		cap     *AudioCapability
		rate    int
		accepts bool
	}{
		{"nil capability accepts any", nil, 16000, true},
		{"empty rates accepts any", &AudioCapability{}, 44100, true},
		{"matching rate", &AudioCapability{SampleRates: []int{16000}}, 16000, true},
		{"non-matching rate", &AudioCapability{SampleRates: []int{16000}}, 44100, false},
		{"multiple rates - match", &AudioCapability{SampleRates: []int{16000, 24000, 44100}}, 24000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.AcceptsSampleRate(tt.rate); got != tt.accepts {
				t.Errorf("AcceptsSampleRate() = %v, want %v", got, tt.accepts)
			}
		})
	}
}

func TestAudioCapability_AcceptsChannels(t *testing.T) {
	tests := []struct {
		name     string
		cap      *AudioCapability
		channels int
		accepts  bool
	}{
		{"nil capability accepts any", nil, 2, true},
		{"empty channels accepts any", &AudioCapability{}, 2, true},
		{"matching channels", &AudioCapability{Channels: []int{1}}, 1, true},
		{"non-matching channels", &AudioCapability{Channels: []int{1}}, 2, false},
		{"multiple channels - match", &AudioCapability{Channels: []int{1, 2}}, 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.AcceptsChannels(tt.channels); got != tt.accepts {
				t.Errorf("AcceptsChannels() = %v, want %v", got, tt.accepts)
			}
		})
	}
}

func TestAudioCapability_AcceptsAudio(t *testing.T) {
	tests := []struct {
		name    string
		cap     *AudioCapability
		audio   *AudioData
		accepts bool
	}{
		{"nil audio", &AudioCapability{Formats: []AudioFormat{AudioFormatPCM16}}, nil, true},
		{"nil capability", nil, &AudioData{Format: AudioFormatOpus}, true},
		{
			"all match",
			&AudioCapability{
				Formats:     []AudioFormat{AudioFormatPCM16},
				SampleRates: []int{16000},
				Channels:    []int{1},
			},
			&AudioData{Format: AudioFormatPCM16, SampleRate: 16000, Channels: 1},
			true,
		},
		{
			"format mismatch",
			&AudioCapability{Formats: []AudioFormat{AudioFormatPCM16}},
			&AudioData{Format: AudioFormatOpus},
			false,
		},
		{
			"rate mismatch",
			&AudioCapability{SampleRates: []int{16000}},
			&AudioData{SampleRate: 44100},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.AcceptsAudio(tt.audio); got != tt.accepts {
				t.Errorf("AcceptsAudio() = %v, want %v", got, tt.accepts)
			}
		})
	}
}

func TestCapabilities_AcceptsContentType(t *testing.T) {
	tests := []struct {
		name    string
		cap     *Capabilities
		ct      ContentType
		accepts bool
	}{
		{"nil capability accepts any", nil, ContentTypeAudio, true},
		{"empty types accepts any", &Capabilities{}, ContentTypeText, true},
		{"any accepts all", &Capabilities{ContentTypes: []ContentType{ContentTypeAny}}, ContentTypeVideo, true},
		{"matching type", &Capabilities{ContentTypes: []ContentType{ContentTypeAudio}}, ContentTypeAudio, true},
		{"non-matching type", &Capabilities{ContentTypes: []ContentType{ContentTypeAudio}}, ContentTypeText, false},
		{
			"multiple types - match",
			&Capabilities{ContentTypes: []ContentType{ContentTypeText, ContentTypeAudio}},
			ContentTypeAudio,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.AcceptsContentType(tt.ct); got != tt.accepts {
				t.Errorf("AcceptsContentType() = %v, want %v", got, tt.accepts)
			}
		})
	}
}

func TestCapabilities_AcceptsElement(t *testing.T) {
	text := "hello"
	tests := []struct {
		name    string
		cap     *Capabilities
		elem    *StreamElement
		accepts bool
	}{
		{"nil capability", nil, &StreamElement{Text: &text}, true},
		{"nil element", &Capabilities{ContentTypes: []ContentType{ContentTypeText}}, nil, true},
		{
			"text element accepted",
			&Capabilities{ContentTypes: []ContentType{ContentTypeText}},
			&StreamElement{Text: &text},
			true,
		},
		{
			"text element rejected",
			&Capabilities{ContentTypes: []ContentType{ContentTypeAudio}},
			&StreamElement{Text: &text},
			false,
		},
		{
			"audio element with format check - pass",
			&Capabilities{
				ContentTypes: []ContentType{ContentTypeAudio},
				Audio:        &AudioCapability{Formats: []AudioFormat{AudioFormatPCM16}},
			},
			&StreamElement{Audio: &AudioData{Format: AudioFormatPCM16}},
			true,
		},
		{
			"audio element with format check - fail",
			&Capabilities{
				ContentTypes: []ContentType{ContentTypeAudio},
				Audio:        &AudioCapability{Formats: []AudioFormat{AudioFormatPCM16}},
			},
			&StreamElement{Audio: &AudioData{Format: AudioFormatOpus}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.AcceptsElement(tt.elem); got != tt.accepts {
				t.Errorf("AcceptsElement() = %v, want %v", got, tt.accepts)
			}
		})
	}
}

func TestCapabilityHelpers(t *testing.T) {
	t.Run("AnyCapabilities", func(t *testing.T) {
		cap := AnyCapabilities()
		if !cap.AcceptsContentType(ContentTypeAudio) {
			t.Error("AnyCapabilities should accept audio")
		}
		if !cap.AcceptsContentType(ContentTypeVideo) {
			t.Error("AnyCapabilities should accept video")
		}
	})

	t.Run("TextCapabilities", func(t *testing.T) {
		cap := TextCapabilities()
		if !cap.AcceptsContentType(ContentTypeText) {
			t.Error("TextCapabilities should accept text")
		}
		if cap.AcceptsContentType(ContentTypeAudio) {
			t.Error("TextCapabilities should not accept audio")
		}
	})

	t.Run("AudioCapabilities", func(t *testing.T) {
		cap := AudioCapabilities(
			[]AudioFormat{AudioFormatPCM16},
			[]int{16000},
			[]int{1},
		)
		if !cap.AcceptsContentType(ContentTypeAudio) {
			t.Error("AudioCapabilities should accept audio")
		}
		if cap.Audio == nil {
			t.Fatal("AudioCapabilities should have Audio capability")
		}
		if !cap.Audio.AcceptsFormat(AudioFormatPCM16) {
			t.Error("Should accept PCM16")
		}
		if cap.Audio.AcceptsFormat(AudioFormatOpus) {
			t.Error("Should not accept Opus")
		}
	})

	t.Run("MessageCapabilities", func(t *testing.T) {
		cap := MessageCapabilities()
		if !cap.AcceptsContentType(ContentTypeMessage) {
			t.Error("MessageCapabilities should accept message")
		}
	})
}
