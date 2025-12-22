package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestProvider_SupportsStreamInput(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		wantLen  int
		wantType string
	}{
		{
			name:     "realtime model",
			model:    "gpt-4o-realtime-preview",
			wantLen:  1,
			wantType: types.ContentTypeAudio,
		},
		{
			name:     "realtime model variant",
			model:    "gpt-4o-realtime-preview-2024-12-17",
			wantLen:  1,
			wantType: types.ContentTypeAudio,
		},
		{
			name:    "non-realtime model",
			model:   "gpt-4o",
			wantLen: 0,
		},
		{
			name:    "gpt-4",
			model:   "gpt-4",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider("test", tt.model, "https://api.openai.com", providers.ProviderDefaults{}, false)
			mediaTypes := p.SupportsStreamInput()

			if len(mediaTypes) != tt.wantLen {
				t.Errorf("expected %d media types, got %d", tt.wantLen, len(mediaTypes))
			}

			if tt.wantLen > 0 && mediaTypes[0] != tt.wantType {
				t.Errorf("expected media type %s, got %s", tt.wantType, mediaTypes[0])
			}
		})
	}
}

func TestProvider_GetStreamingCapabilities(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)
	caps := p.GetStreamingCapabilities()

	if len(caps.SupportedMediaTypes) == 0 {
		t.Error("expected supported media types")
	}

	if !caps.BidirectionalSupport {
		t.Error("expected bidirectional support")
	}

	if caps.Audio == nil {
		t.Fatal("expected audio capabilities")
	}

	if caps.Audio.PreferredSampleRate != 24000 {
		t.Errorf("expected sample rate 24000, got %d", caps.Audio.PreferredSampleRate)
	}

	if caps.Audio.PreferredEncoding != "pcm16" {
		t.Errorf("expected encoding pcm16, got %s", caps.Audio.PreferredEncoding)
	}
}

func TestProvider_validateStreamRequest(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	tests := []struct {
		name    string
		config  types.StreamingMediaConfig
		wantErr bool
	}{
		{
			name: "valid audio config",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 24000,
				Encoding:   "pcm16",
				Channels:   1,
				ChunkSize:  1024,
			},
			wantErr: false,
		},
		{
			name: "valid audio with different sample rate (warning only)",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 16000,
				Encoding:   "pcm",
				Channels:   1,
				ChunkSize:  1024,
			},
			wantErr: false,
		},
		{
			name: "video not supported",
			config: types.StreamingMediaConfig{
				Type:      types.ContentTypeVideo,
				ChunkSize: 1024,
			},
			wantErr: true,
		},
		{
			name: "text not supported",
			config: types.StreamingMediaConfig{
				Type:      types.ContentTypeText,
				ChunkSize: 1024,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &providers.StreamingInputConfig{
				Config: tt.config,
			}
			err := p.validateStreamRequest(req)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestProvider_buildRealtimeSessionConfig(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	t.Run("with system instruction", func(t *testing.T) {
		req := &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio,
			},
			SystemInstruction: "You are a helpful assistant.",
		}

		config := p.buildRealtimeSessionConfig(req)

		if config.Instructions != "You are a helpful assistant." {
			t.Errorf("expected system instruction, got %s", config.Instructions)
		}
	})

	t.Run("uses realtime model", func(t *testing.T) {
		req := &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio,
			},
		}

		config := p.buildRealtimeSessionConfig(req)

		if config.Model != "gpt-4o-realtime-preview" {
			t.Errorf("expected realtime model, got %s", config.Model)
		}
	})
}

func TestProvider_applyStreamMetadata(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	t.Run("applies voice", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"voice": "shimmer",
		}

		p.applyStreamMetadata(metadata, &config)

		if config.Voice != "shimmer" {
			t.Errorf("expected voice shimmer, got %s", config.Voice)
		}
	})

	t.Run("applies modalities as []string", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"modalities": []string{"text"},
		}

		p.applyStreamMetadata(metadata, &config)

		if len(config.Modalities) != 1 || config.Modalities[0] != "text" {
			t.Errorf("expected text modality, got %v", config.Modalities)
		}
	})

	t.Run("applies modalities as []interface{}", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"modalities": []interface{}{"audio"},
		}

		p.applyStreamMetadata(metadata, &config)

		if len(config.Modalities) != 1 || config.Modalities[0] != "audio" {
			t.Errorf("expected audio modality, got %v", config.Modalities)
		}
	})

	t.Run("enables input transcription by default", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{}

		p.applyStreamMetadata(metadata, &config)

		if config.InputAudioTranscription == nil {
			t.Error("expected input transcription config to be enabled by default")
		}
	})

	t.Run("disables input transcription when explicitly set to false", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"input_transcription": false,
		}

		p.applyStreamMetadata(metadata, &config)

		if config.InputAudioTranscription != nil {
			t.Error("expected input transcription to be disabled")
		}
	})

	t.Run("disables VAD", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"vad_disabled": true,
		}

		p.applyStreamMetadata(metadata, &config)

		if config.TurnDetection != nil {
			t.Error("expected turn detection to be nil")
		}
	})

	t.Run("applies temperature", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"temperature": 0.5,
		}

		p.applyStreamMetadata(metadata, &config)

		if config.Temperature != 0.5 {
			t.Errorf("expected temperature 0.5, got %f", config.Temperature)
		}
	})

	t.Run("nil metadata is safe", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		p.applyStreamMetadata(nil, &config)
		// Should not panic
	})
}

func TestProvider_applyStreamTools(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	t.Run("applies tools", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		tools := []providers.StreamingToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		}

		p.applyStreamTools(tools, &config)

		if len(config.Tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(config.Tools))
		}

		if config.Tools[0].Name != "get_weather" {
			t.Errorf("expected tool name get_weather, got %s", config.Tools[0].Name)
		}

		if config.Tools[0].Type != "function" {
			t.Errorf("expected tool type function, got %s", config.Tools[0].Type)
		}
	})

	t.Run("empty tools is safe", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		p.applyStreamTools(nil, &config)

		if len(config.Tools) != 0 {
			t.Errorf("expected 0 tools, got %d", len(config.Tools))
		}
	})
}

func TestProvider_StreamInputSupport_Interface(t *testing.T) {
	// Verify Provider implements StreamInputSupport
	var _ providers.StreamInputSupport = (*Provider)(nil)
}
