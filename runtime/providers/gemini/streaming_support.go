package gemini

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Ensure GeminiProvider implements StreamInputSupport
var _ providers.StreamInputSupport = (*GeminiProvider)(nil)

// CreateStreamSession creates a new bidirectional streaming session with Gemini Live API
//
// Response Modalities:
// By default, the session is configured to return TEXT responses only.
// To request audio responses, pass "response_modalities" in the request metadata:
//
//	req := providers.StreamInputRequest{
//	    Config: config,
//	    Metadata: map[string]interface{}{
//	        "response_modalities": []string{"AUDIO"},        // Audio only
//	        // OR
//	        "response_modalities": []string{"TEXT", "AUDIO"}, // Both text and audio
//	    },
//	}
//
// Audio responses will be delivered in the StreamChunk.Metadata["audio_data"] field as base64-encoded PCM.
func (p *GeminiProvider) CreateStreamSession(ctx context.Context, req providers.StreamInputRequest) (providers.StreamInputSession, error) {
	// Validate configuration
	if err := req.Config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid stream configuration: %w", err)
	}

	// Check if media type is supported
	if req.Config.Type != types.ContentTypeAudio {
		return nil, fmt.Errorf("unsupported media type: %s (only audio is supported)", req.Config.Type)
	}

	// Validate audio config against Gemini requirements
	encoder := NewAudioEncoder()
	if err := encoder.ValidateConfig(&req.Config); err != nil {
		return nil, fmt.Errorf("invalid audio configuration: %w", err)
	}

	// Construct WebSocket URL for Gemini Live API
	// Format: wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent
	// Note: API key is passed via x-goog-api-key header, not as query parameter
	wsURL := "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"

	// Configure session with model and response modalities
	config := StreamSessionConfig{
		Model: p.Model,
	}

	// Check metadata for response modalities configuration
	if req.Metadata != nil {
		if modalities, ok := req.Metadata["response_modalities"].([]string); ok {
			config.ResponseModalities = modalities
		} else if modalities, ok := req.Metadata["response_modalities"].([]interface{}); ok {
			// Handle case where metadata comes as []interface{}
			config.ResponseModalities = make([]string, 0, len(modalities))
			for _, m := range modalities {
				if s, ok := m.(string); ok {
					config.ResponseModalities = append(config.ResponseModalities, s)
				}
			}
		}
	}

	// Default to TEXT if not specified
	if len(config.ResponseModalities) == 0 {
		config.ResponseModalities = []string{"TEXT"}
	}

	// Create session with configuration
	session, err := NewGeminiStreamSession(ctx, wsURL, p.ApiKey, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream session: %w", err)
	}

	// Note: System messages and initial text would be sent via SendText() after creation
	// The Gemini Live API doesn't have a separate setup phase for these

	return session, nil
}

// SupportsStreamInput returns the media types supported for streaming input
func (p *GeminiProvider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio}
}

// GetStreamingCapabilities returns detailed information about Gemini's streaming support
func (p *GeminiProvider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{
		SupportedMediaTypes: []string{types.ContentTypeAudio},
		Audio: &providers.AudioStreamingCapabilities{
			SupportedEncodings:   []string{"pcm_linear16"},
			SupportedSampleRates: []int{16000},
			SupportedChannels:    []int{1}, // mono only
			SupportedBitDepths:   []int{16},
			PreferredEncoding:    "pcm_linear16",
			PreferredSampleRate:  16000,
		},
		Video:                nil, // Video not supported yet
		BidirectionalSupport: true,
		MaxSessionDuration:   0,     // No limit
		MinChunkSize:         160,   // 10ms at 16kHz
		MaxChunkSize:         32000, // ~2 seconds at 16kHz
	}
}
