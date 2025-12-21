// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Ensure Provider implements StreamInputSupport
var _ providers.StreamInputSupport = (*Provider)(nil)

// validateStreamRequest validates the streaming input configuration.
func (p *Provider) validateStreamRequest(req *providers.StreamingInputConfig) error {
	if err := req.Config.Validate(); err != nil {
		return fmt.Errorf("invalid stream configuration: %w", err)
	}

	if req.Config.Type != types.ContentTypeAudio {
		return fmt.Errorf("unsupported media type: %s (only audio is supported)", req.Config.Type)
	}

	// Validate sample rate (OpenAI Realtime only supports 24kHz)
	if req.Config.SampleRate != 0 && req.Config.SampleRate != DefaultRealtimeSampleRate {
		logger.Warn("OpenAI Realtime: non-standard sample rate",
			"requested", req.Config.SampleRate,
			"supported", DefaultRealtimeSampleRate)
	}

	return nil
}

// buildRealtimeSessionConfig creates a base session configuration.
func (p *Provider) buildRealtimeSessionConfig(req *providers.StreamingInputConfig) RealtimeSessionConfig {
	config := DefaultRealtimeSessionConfig()

	// Use provider model if it's a realtime model
	if strings.Contains(p.model, "realtime") {
		config.Model = p.model
	}

	if req.SystemInstruction != "" {
		config.Instructions = req.SystemInstruction
	}

	// Apply pricing from provider defaults
	if p.defaults.Pricing.InputCostPer1K > 0 && p.defaults.Pricing.OutputCostPer1K > 0 {
		// Note: Realtime API has different pricing; this is a fallback
		config.Temperature = 0.8 // Default temperature
	}

	return config
}

// applyStreamMetadata extracts configuration from request metadata.
func (p *Provider) applyStreamMetadata(metadata map[string]interface{}, config *RealtimeSessionConfig) {
	if metadata == nil {
		return
	}

	// Voice selection
	if voice, ok := metadata["voice"].(string); ok {
		config.Voice = voice
	}

	// Response modalities
	switch modalities := metadata["modalities"].(type) {
	case []string:
		config.Modalities = modalities
	case []interface{}:
		config.Modalities = make([]string, 0, len(modalities))
		for _, m := range modalities {
			if s, ok := m.(string); ok {
				config.Modalities = append(config.Modalities, s)
			}
		}
	}

	// Input audio transcription
	if enableTranscription, ok := metadata["input_transcription"].(bool); ok && enableTranscription {
		config.InputAudioTranscription = &TranscriptionConfig{
			Model: "whisper-1",
		}
	}

	// Turn detection / VAD settings
	if vadDisabled, ok := metadata["vad_disabled"].(bool); ok && vadDisabled {
		config.TurnDetection = nil // Disable VAD for manual turn control
	}

	// Temperature
	if temp, ok := metadata["temperature"].(float64); ok {
		config.Temperature = temp
	}
}

// applyStreamTools converts and applies tools configuration.
func (p *Provider) applyStreamTools(tools []providers.StreamingToolDefinition, config *RealtimeSessionConfig) {
	if len(tools) == 0 {
		return
	}

	config.Tools = make([]RealtimeToolDefinition, len(tools))
	for i, tool := range tools {
		config.Tools[i] = RealtimeToolDefinition{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		}
	}
	logger.Debug("CreateStreamSession: tools configured", "tool_count", len(config.Tools))
}

// SupportsStreamInput returns the media types supported for streaming input.
func (p *Provider) SupportsStreamInput() []string {
	// Only return audio if the model supports realtime
	if strings.Contains(p.model, "realtime") {
		return []string{types.ContentTypeAudio}
	}
	// For non-realtime models, we could potentially support text streaming
	// but return empty for now to indicate no bidirectional streaming support
	return []string{}
}

// GetStreamingCapabilities returns detailed information about OpenAI's streaming support.
func (p *Provider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return RealtimeStreamingCapabilities()
}
