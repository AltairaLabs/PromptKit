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
	// Note: API key is passed via Authorization header, not as query parameter
	wsURL := "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"

	// Create session
	session, err := NewGeminiStreamSession(ctx, wsURL, p.ApiKey)
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
