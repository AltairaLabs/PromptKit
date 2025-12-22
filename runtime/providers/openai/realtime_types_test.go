package openai

import (
	"testing"
)

func TestDefaultRealtimeSessionConfig(t *testing.T) {
	config := DefaultRealtimeSessionConfig()

	if config.Model != "gpt-4o-realtime-preview" {
		t.Errorf("expected model gpt-4o-realtime-preview, got %s", config.Model)
	}

	if len(config.Modalities) != 2 {
		t.Errorf("expected 2 modalities, got %d", len(config.Modalities))
	}

	if config.Voice != "alloy" {
		t.Errorf("expected voice alloy, got %s", config.Voice)
	}

	if config.InputAudioFormat != "pcm16" {
		t.Errorf("expected input format pcm16, got %s", config.InputAudioFormat)
	}

	if config.OutputAudioFormat != "pcm16" {
		t.Errorf("expected output format pcm16, got %s", config.OutputAudioFormat)
	}

	if config.Temperature != 0.8 {
		t.Errorf("expected temperature 0.8, got %f", config.Temperature)
	}

	if config.TurnDetection == nil {
		t.Fatal("expected turn detection config")
	}

	if config.TurnDetection.Type != "server_vad" {
		t.Errorf("expected turn detection type server_vad, got %s", config.TurnDetection.Type)
	}

	if config.TurnDetection.Threshold != 0.5 {
		t.Errorf("expected threshold 0.5, got %f", config.TurnDetection.Threshold)
	}

	if !config.TurnDetection.CreateResponse {
		t.Error("expected create_response to be true")
	}
}

func TestRealtimeStreamingCapabilities(t *testing.T) {
	caps := RealtimeStreamingCapabilities()

	if len(caps.SupportedMediaTypes) != 1 || caps.SupportedMediaTypes[0] != "audio" {
		t.Errorf("expected audio media type, got %v", caps.SupportedMediaTypes)
	}

	if !caps.BidirectionalSupport {
		t.Error("expected bidirectional support")
	}

	if caps.Audio == nil {
		t.Fatal("expected audio capabilities")
	}

	if len(caps.Audio.SupportedEncodings) == 0 {
		t.Error("expected supported encodings")
	}

	foundPCM16 := false
	for _, enc := range caps.Audio.SupportedEncodings {
		if enc == "pcm16" {
			foundPCM16 = true
			break
		}
	}
	if !foundPCM16 {
		t.Error("expected pcm16 in supported encodings")
	}

	if caps.Audio.PreferredSampleRate != DefaultRealtimeSampleRate {
		t.Errorf("expected sample rate %d, got %d", DefaultRealtimeSampleRate, caps.Audio.PreferredSampleRate)
	}

	if caps.Audio.PreferredEncoding != "pcm16" {
		t.Errorf("expected preferred encoding pcm16, got %s", caps.Audio.PreferredEncoding)
	}
}

func TestRealtimeConstants(t *testing.T) {
	if RealtimeAPIEndpoint != "wss://api.openai.com/v1/realtime" {
		t.Errorf("unexpected API endpoint: %s", RealtimeAPIEndpoint)
	}

	if RealtimeBetaHeader != "realtime=v1" {
		t.Errorf("unexpected beta header: %s", RealtimeBetaHeader)
	}

	if DefaultRealtimeSampleRate != 24000 {
		t.Errorf("expected sample rate 24000, got %d", DefaultRealtimeSampleRate)
	}

	if DefaultRealtimeChannels != 1 {
		t.Errorf("expected 1 channel, got %d", DefaultRealtimeChannels)
	}

	if DefaultRealtimeBitDepth != 16 {
		t.Errorf("expected 16-bit depth, got %d", DefaultRealtimeBitDepth)
	}
}

func TestRealtimeSessionConfig_Modalities(t *testing.T) {
	tests := []struct {
		name       string
		modalities []string
		wantLen    int
	}{
		{
			name:       "text only",
			modalities: []string{"text"},
			wantLen:    1,
		},
		{
			name:       "audio only",
			modalities: []string{"audio"},
			wantLen:    1,
		},
		{
			name:       "text and audio",
			modalities: []string{"text", "audio"},
			wantLen:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := RealtimeSessionConfig{
				Modalities: tt.modalities,
			}
			if len(config.Modalities) != tt.wantLen {
				t.Errorf("expected %d modalities, got %d", tt.wantLen, len(config.Modalities))
			}
		})
	}
}

func TestRealtimeToolDefinition(t *testing.T) {
	tool := RealtimeToolDefinition{
		Type:        "function",
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g. San Francisco, CA",
				},
			},
			"required": []string{"location"},
		},
	}

	if tool.Type != "function" {
		t.Errorf("expected type function, got %s", tool.Type)
	}

	if tool.Name != "get_weather" {
		t.Errorf("expected name get_weather, got %s", tool.Name)
	}

	if tool.Parameters == nil {
		t.Error("expected parameters")
	}
}

func TestTranscriptionConfig(t *testing.T) {
	config := TranscriptionConfig{
		Model: "whisper-1",
	}

	if config.Model != "whisper-1" {
		t.Errorf("expected model whisper-1, got %s", config.Model)
	}
}

func TestTurnDetectionConfig(t *testing.T) {
	tests := []struct {
		name   string
		config TurnDetectionConfig
	}{
		{
			name: "default VAD",
			config: TurnDetectionConfig{
				Type:              "server_vad",
				Threshold:         0.5,
				PrefixPaddingMs:   300,
				SilenceDurationMs: 500,
				CreateResponse:    true,
			},
		},
		{
			name: "custom threshold",
			config: TurnDetectionConfig{
				Type:              "server_vad",
				Threshold:         0.8,
				PrefixPaddingMs:   200,
				SilenceDurationMs: 700,
				CreateResponse:    false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.Type != "server_vad" {
				t.Errorf("expected type server_vad, got %s", tt.config.Type)
			}
			if tt.config.Threshold < 0 || tt.config.Threshold > 1 {
				t.Errorf("threshold out of range: %f", tt.config.Threshold)
			}
		})
	}
}
