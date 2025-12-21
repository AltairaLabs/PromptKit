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
)

// Ensure GeminiProvider implements StreamInputSupport
var _ providers.StreamInputSupport = (*Provider)(nil)

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

	// Configure session with model, response modalities, system instruction, and pricing
	config := StreamSessionConfig{
		Model:             p.Model,
		SystemInstruction: req.SystemInstruction,
		AutoReconnect:     true, // Enable auto-reconnect by default for resilience
		MaxReconnectTries: defaultMaxReconnectTries,
	}

	// Set pricing from provider defaults, or use model-based defaults
	if p.Defaults.Pricing.InputCostPer1K > 0 && p.Defaults.Pricing.OutputCostPer1K > 0 {
		config.InputCostPer1K = p.Defaults.Pricing.InputCostPer1K
		config.OutputCostPer1K = p.Defaults.Pricing.OutputCostPer1K
	} else {
		// Use model-based default pricing
		config.InputCostPer1K, config.OutputCostPer1K, _ = geminiPricing(p.Model)
	}

	// Check metadata for response modalities configuration
	if req.Metadata != nil {
		switch modalities := req.Metadata["response_modalities"].(type) {
		case []string:
			config.ResponseModalities = modalities
		case []interface{}:
			// Handle case where metadata comes as []interface{}
			config.ResponseModalities = make([]string, 0, len(modalities))
			for _, m := range modalities {
				if s, ok := m.(string); ok {
					config.ResponseModalities = append(config.ResponseModalities, s)
				}
			}
		}

		// Check for VAD disabled mode (manual turn control)
		// When vad_disabled=true, automatic VAD is disabled and callers must use
		// activityStart/activityEnd signals for explicit turn control.
		// This is useful for pre-recorded audio (like TTS selfplay) where we know
		// exactly when the audio starts and ends, avoiding false turn detections
		// from natural speech pauses.
		if vadDisabled, ok := req.Metadata["vad_disabled"].(bool); ok && vadDisabled {
			config.VAD = &VADConfig{
				Disabled: true,
			}
		}

		// Note: We intentionally do NOT pass custom VAD threshold configuration to Gemini.
		// Gemini's default VAD thresholds work well, and custom values (especially
		// custom silence_duration_ms and prefix_padding_ms) can cause Gemini to
		// not respond at all. The scenario's VAD config is used for local turn
		// detection only, not passed to the provider. However, we DO support
		// vad_disabled=true for explicit turn control.
	}

	// Convert tools from StreamingInputConfig to StreamSessionConfig format
	// When tools are provided, Gemini will return structured tool calls instead of
	// speaking them as text
	if len(req.Tools) > 0 {
		config.Tools = make([]ToolDefinition, len(req.Tools))
		for i, tool := range req.Tools {
			config.Tools[i] = ToolDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			}
		}
		logger.Debug("CreateStreamSession: tools configured", "tool_count", len(config.Tools))
	}

	// Default to TEXT if not specified
	if len(config.ResponseModalities) == 0 {
		config.ResponseModalities = []string{"TEXT"}
	}

	// Create session with configuration
	session, err := NewStreamSession(ctx, wsURL, p.ApiKey, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream session: %w", err)
	}

	return session, nil
}

// SupportsStreamInput returns the media types supported for streaming input
func (p *Provider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio}
}

// GetStreamingCapabilities returns detailed information about Gemini's streaming support
func (p *Provider) GetStreamingCapabilities() providers.StreamingCapabilities {
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
