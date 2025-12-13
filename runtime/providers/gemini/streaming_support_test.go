package gemini

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestGeminiProvider_ImplementsStreamInputSupport(t *testing.T) {
	provider := NewProvider("test", "gemini-2.0-flash-exp", "https://api.test.com", providers.ProviderDefaults{}, false)

	// Type assertion to ensure interface is implemented
	_, ok := interface{}(provider).(providers.StreamInputSupport)
	if !ok {
		t.Fatal("GeminiProvider does not implement StreamInputSupport interface")
	}
}

func TestGeminiProvider_SupportsStreamInput(t *testing.T) {
	provider := NewProvider("test", "gemini-2.0-flash-exp", "https://api.test.com", providers.ProviderDefaults{}, false)

	supported := provider.SupportsStreamInput()

	if len(supported) == 0 {
		t.Fatal("expected at least one supported media type")
	}

	// Should support audio
	foundAudio := false
	for _, mediaType := range supported {
		if mediaType == types.ContentTypeAudio {
			foundAudio = true
			break
		}
	}

	if !foundAudio {
		t.Error("expected audio to be supported")
	}
}

func TestGeminiProvider_GetStreamingCapabilities(t *testing.T) {
	provider := NewProvider("test", "gemini-2.0-flash-exp", "https://api.test.com", providers.ProviderDefaults{}, false)

	caps := provider.GetStreamingCapabilities()

	// Check supported media types
	if len(caps.SupportedMediaTypes) == 0 {
		t.Fatal("expected at least one supported media type")
	}

	// Check audio capabilities
	if caps.Audio == nil {
		t.Fatal("expected audio capabilities to be present")
	}

	// Verify encodings
	if len(caps.Audio.SupportedEncodings) == 0 {
		t.Error("expected at least one supported audio encoding")
	}

	// Verify sample rates
	if len(caps.Audio.SupportedSampleRates) == 0 {
		t.Error("expected at least one supported sample rate")
	}

	// Verify 16kHz is supported (Gemini requirement)
	found16k := false
	for _, rate := range caps.Audio.SupportedSampleRates {
		if rate == 16000 {
			found16k = true
			break
		}
	}
	if !found16k {
		t.Error("expected 16000 Hz to be supported")
	}

	// Verify channels
	if len(caps.Audio.SupportedChannels) == 0 {
		t.Error("expected at least one supported channel count")
	}

	// Verify mono (1 channel) is supported
	foundMono := false
	for _, channels := range caps.Audio.SupportedChannels {
		if channels == 1 {
			foundMono = true
			break
		}
	}
	if !foundMono {
		t.Error("expected mono (1 channel) to be supported")
	}

	// Verify bit depths
	if len(caps.Audio.SupportedBitDepths) == 0 {
		t.Error("expected at least one supported bit depth")
	}

	// Verify 16-bit is supported
	found16bit := false
	for _, depth := range caps.Audio.SupportedBitDepths {
		if depth == 16 {
			found16bit = true
			break
		}
	}
	if !found16bit {
		t.Error("expected 16-bit depth to be supported")
	}

	// Check preferred settings
	if caps.Audio.PreferredEncoding == "" {
		t.Error("expected preferred encoding to be set")
	}

	if caps.Audio.PreferredSampleRate == 0 {
		t.Error("expected preferred sample rate to be set")
	}

	// Verify bidirectional support
	if !caps.BidirectionalSupport {
		t.Error("expected bidirectional support to be true")
	}

	// Check chunk size limits
	if caps.MinChunkSize == 0 {
		t.Error("expected min chunk size to be set")
	}

	if caps.MaxChunkSize == 0 {
		t.Error("expected max chunk size to be set")
	}

	if caps.MinChunkSize >= caps.MaxChunkSize {
		t.Errorf("min chunk size (%d) should be less than max chunk size (%d)", caps.MinChunkSize, caps.MaxChunkSize)
	}
}

func TestGeminiProvider_CreateStreamSession_InvalidConfig(t *testing.T) {
	provider := NewProvider("test", "gemini-2.0-flash-exp", "https://api.test.com", providers.ProviderDefaults{}, false)
	ctx := context.Background()

	tests := []struct {
		name   string
		config types.StreamingMediaConfig
	}{
		{
			name: "unsupported media type",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeVideo,
				ChunkSize:  1024,
				SampleRate: 16000,
				Channels:   1,
				BitDepth:   16,
			},
		},
		{
			name: "invalid sample rate",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				ChunkSize:  1024,
				SampleRate: 44100, // Not supported by Gemini
				Channels:   1,
				BitDepth:   16,
				Encoding:   "pcm_linear16",
			},
		},
		{
			name: "invalid channels",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				ChunkSize:  1024,
				SampleRate: 16000,
				Channels:   2, // Stereo not supported
				BitDepth:   16,
				Encoding:   "pcm_linear16",
			},
		},
		{
			name: "invalid bit depth",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				ChunkSize:  1024,
				SampleRate: 16000,
				Channels:   1,
				BitDepth:   24, // 24-bit not supported
				Encoding:   "pcm_linear16",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := providers.StreamInputRequest{
				Config: tt.config,
			}

			_, err := provider.CreateStreamSession(ctx, &req)
			if err == nil {
				t.Fatal("expected error for invalid configuration, got nil")
			}
		})
	}
}

func TestGeminiProvider_CreateStreamSession_ValidConfig(t *testing.T) {
	// Skip if no API key available (for CI/CD)
	provider := NewProvider("test", "gemini-2.0-flash-exp", "https://api.test.com", providers.ProviderDefaults{}, false)
	if provider.ApiKey == "" {
		t.Skip("Skipping test: GEMINI_API_KEY not set")
	}

	ctx := context.Background()

	config := types.StreamingMediaConfig{
		Type:       types.ContentTypeAudio,
		ChunkSize:  3200,
		SampleRate: 16000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm_linear16",
	}

	req := providers.StreamInputRequest{
		Config: config,
	}

	session, err := provider.CreateStreamSession(ctx, &req)
	if err != nil {
		// Check if this is a Live API access error
		errMsg := err.Error()
		if strings.Contains(errMsg, "API key not valid") || strings.Contains(errMsg, "websocket: close 1007") {
			t.Skipf("Skipping test: API key does not have Gemini Live API access. Error: %v", err)
		}
		t.Fatalf("unexpected error creating session: %v", err)
	}

	if session == nil {
		t.Fatal("expected session to be non-nil")
	}

	// Clean up
	if err := session.Close(); err != nil {
		t.Errorf("error closing session: %v", err)
	}
}

func TestGeminiProvider_CreateStreamSession_ResponseModalities(t *testing.T) {
	provider := NewProvider("test", "gemini-2.0-flash-exp", "https://api.test.com", providers.ProviderDefaults{}, false)
	ctx := context.Background()

	baseConfig := types.StreamingMediaConfig{
		Type:       types.ContentTypeAudio,
		ChunkSize:  3200,
		SampleRate: 16000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm_linear16",
	}

	tests := []struct {
		name                string
		metadata            map[string]interface{}
		expectedModality    []string
		shouldCreateSession bool
	}{
		{
			name:                "default TEXT modality",
			metadata:            nil,
			expectedModality:    []string{"TEXT"},
			shouldCreateSession: false, // Will fail without API key but validates config path
		},
		{
			name: "AUDIO modality - string slice",
			metadata: map[string]interface{}{
				"response_modalities": []string{"AUDIO"},
			},
			expectedModality:    []string{"AUDIO"},
			shouldCreateSession: false,
		},
		{
			name: "TEXT and AUDIO modalities - interface slice",
			metadata: map[string]interface{}{
				"response_modalities": []interface{}{"TEXT", "AUDIO"},
			},
			expectedModality:    []string{"TEXT", "AUDIO"},
			shouldCreateSession: false,
		},
		{
			name: "empty modalities metadata",
			metadata: map[string]interface{}{
				"response_modalities": []string{},
			},
			expectedModality:    []string{"TEXT"}, // Should default to TEXT
			shouldCreateSession: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := providers.StreamInputRequest{
				Config:   baseConfig,
				Metadata: tt.metadata,
			}

			// This will attempt to create a session and fail due to no API key
			// But we're testing that the config path is executed correctly
			_, err := provider.CreateStreamSession(ctx, &req)

			// Should get an error about websocket connection, not about validation
			if err != nil {
				errMsg := err.Error()
				// Make sure we didn't fail on config validation
				if !strings.Contains(errMsg, "failed to create stream session") && !strings.Contains(errMsg, "websocket") && !strings.Contains(errMsg, "connection") {
					t.Errorf("Expected connection error, got: %v", err)
				}
			}
		})
	}
}

func TestGeminiProvider_CreateStreamSession_EmptyConfig(t *testing.T) {
	provider := NewProvider("test", "gemini-2.0-flash-exp", "https://api.test.com", providers.ProviderDefaults{}, false)
	ctx := context.Background()

	req := providers.StreamInputRequest{
		Config: types.StreamingMediaConfig{
			// Empty config
		},
	}

	_, err := provider.CreateStreamSession(ctx, &req)
	if err == nil {
		t.Fatal("expected error for empty configuration, got nil")
	}

	if !strings.Contains(err.Error(), "invalid stream configuration") {
		t.Errorf("expected 'invalid stream configuration' error, got: %v", err)
	}
}
