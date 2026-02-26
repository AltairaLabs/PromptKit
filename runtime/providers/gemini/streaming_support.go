package gemini

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// defaultMaxReconnectTries is the default number of reconnection attempts for stream sessions.
	defaultMaxReconnectTries = 3

	// Video resolution constants for streaming capabilities
	videoResHDWidth   = 1920
	videoResHDHeight  = 1080
	videoRes1024      = 1024
	videoResVGAWidth  = 640
	videoResVGAHeight = 480
	videoPreferredFPS = 1 // Gemini Live API processes at 1 FPS
	audioMinChunkSize = 160
	audioMaxChunkSize = 32000
)

// Ensure GeminiProvider implements StreamInputSupport
var _ providers.StreamInputSupport = (*Provider)(nil)

// geminiLiveAPIURL is the WebSocket URL for Gemini Live API
//
//nolint:lll // URL cannot be split
const geminiLiveAPIURL = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"

// CreateStreamSession creates a new bidirectional streaming session with Gemini Live API
//
// Response Modalities:
// By default, the session is configured to return TEXT responses only.
// To request audio responses, pass "response_modalities" in the request metadata:
//
//	req := providers.StreamInputRequest{
//	    Config: config,
//	    Metadata: map[string]interface{}{
//	        "response_modalities": []string{"AUDIO"}, // Audio only (TEXT+AUDIO not supported)
//	    },
//	}
//
// Audio responses will be delivered in the StreamChunk.Metadata["audio_data"] field as base64-encoded PCM.
func (p *Provider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	if err := p.validateStreamRequest(req); err != nil {
		return nil, err
	}

	config := p.buildStreamSessionConfig(req)
	p.applyMetadataConfig(req.Metadata, &config)
	p.applyToolsConfig(req.Tools, &config)

	if len(config.ResponseModalities) == 0 {
		config.ResponseModalities = []string{"TEXT"}
	}

	session, err := NewStreamSession(ctx, geminiLiveAPIURL, p.apiKey, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream session: %w", err)
	}

	return session, nil
}

// validateStreamRequest validates the streaming input configuration
func (p *Provider) validateStreamRequest(req *providers.StreamingInputConfig) error {
	if err := req.Config.Validate(); err != nil {
		return fmt.Errorf("invalid stream configuration: %w", err)
	}

	switch req.Config.Type {
	case types.ContentTypeAudio:
		encoder := NewAudioEncoder()
		if err := encoder.ValidateConfig(&req.Config); err != nil {
			return fmt.Errorf("invalid audio configuration: %w", err)
		}
	case types.ContentTypeImage, types.ContentTypeVideo:
		// Video/image streaming supported via Gemini Live API
		// Validated at frame send time
	default:
		return fmt.Errorf("unsupported media type: %s (supported: audio, video, image)", req.Config.Type)
	}

	return nil
}

// buildStreamSessionConfig creates a base session configuration
func (p *Provider) buildStreamSessionConfig(req *providers.StreamingInputConfig) StreamSessionConfig {
	config := StreamSessionConfig{
		Model:             p.model,
		SystemInstruction: req.SystemInstruction,
		AutoReconnect:     true,
		MaxReconnectTries: defaultMaxReconnectTries,
	}

	p.applyPricingConfig(&config)
	return config
}

// applyPricingConfig sets pricing from provider defaults or model-based defaults
func (p *Provider) applyPricingConfig(config *StreamSessionConfig) {
	if p.defaults.Pricing.InputCostPer1K > 0 && p.defaults.Pricing.OutputCostPer1K > 0 {
		config.InputCostPer1K = p.defaults.Pricing.InputCostPer1K
		config.OutputCostPer1K = p.defaults.Pricing.OutputCostPer1K
	} else {
		config.InputCostPer1K, config.OutputCostPer1K, _ = geminiPricing(p.model)
	}
}

// applyMetadataConfig extracts configuration from request metadata
func (p *Provider) applyMetadataConfig(metadata map[string]interface{}, config *StreamSessionConfig) {
	if metadata == nil {
		return
	}

	p.applyResponseModalities(metadata, config)
	p.applyVADDisabled(metadata, config)
}

// applyResponseModalities extracts response modalities from metadata
func (p *Provider) applyResponseModalities(metadata map[string]interface{}, config *StreamSessionConfig) {
	switch modalities := metadata["response_modalities"].(type) {
	case []string:
		config.ResponseModalities = modalities
	case []interface{}:
		config.ResponseModalities = make([]string, 0, len(modalities))
		for _, m := range modalities {
			if s, ok := m.(string); ok {
				config.ResponseModalities = append(config.ResponseModalities, s)
			}
		}
	}
}

// applyVADDisabled checks for VAD disabled mode in metadata
func (p *Provider) applyVADDisabled(metadata map[string]interface{}, config *StreamSessionConfig) {
	if vadDisabled, ok := metadata["vad_disabled"].(bool); ok && vadDisabled {
		config.VAD = &VADConfig{
			Disabled: true,
		}
	}
}

// applyToolsConfig converts and applies tools configuration
func (p *Provider) applyToolsConfig(tools []providers.StreamingToolDefinition, config *StreamSessionConfig) {
	if len(tools) == 0 {
		return
	}

	config.Tools = make([]ToolDefinition, len(tools))
	for i, tool := range tools {
		config.Tools[i] = ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		}
	}
	logger.Debug("CreateStreamSession: tools configured", "tool_count", len(config.Tools))
}

// SupportsStreamInput returns the media types supported for streaming input
func (p *Provider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio, types.ContentTypeVideo, types.ContentTypeImage}
}

// GetStreamingCapabilities returns detailed information about Gemini's streaming support
func (p *Provider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{
		SupportedMediaTypes: []string{types.ContentTypeAudio, types.ContentTypeVideo, types.ContentTypeImage},
		Audio: &providers.AudioStreamingCapabilities{
			SupportedEncodings:   []string{"pcm_linear16"},
			SupportedSampleRates: []int{16000},
			SupportedChannels:    []int{1}, // mono only
			SupportedBitDepths:   []int{16},
			PreferredEncoding:    "pcm_linear16",
			PreferredSampleRate:  16000,
		},
		Video: &providers.VideoStreamingCapabilities{
			SupportedEncodings: []string{"image/jpeg", "image/png", "image/gif", "image/webp"},
			SupportedResolutions: []providers.VideoResolution{
				{Width: videoResHDWidth, Height: videoResHDHeight},
				{Width: videoRes1024, Height: videoRes1024},
				{Width: videoResVGAWidth, Height: videoResVGAHeight},
			},
			SupportedFrameRates: []int{videoPreferredFPS}, // Gemini Live API processes at 1 FPS
			PreferredEncoding:   "image/jpeg",
			PreferredResolution: providers.VideoResolution{Width: videoRes1024, Height: videoRes1024},
			PreferredFrameRate:  videoPreferredFPS,
		},
		BidirectionalSupport: true,
		MaxSessionDuration:   0, // No limit
		MinChunkSize:         audioMinChunkSize,
		MaxChunkSize:         audioMaxChunkSize,
	}
}
