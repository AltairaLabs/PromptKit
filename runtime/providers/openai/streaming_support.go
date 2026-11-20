// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Ensure Provider implements StreamInputSupport
var _ providers.StreamInputSupport = (*Provider)(nil)

// defaultInputTranscriptionModel transcribes a realtime session's input audio
// unless metadata["input_transcription_model"] names another model.
const defaultInputTranscriptionModel = "whisper-1"

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

	// The session runs the configured model. A fallback default here used to
	// swap any model without "realtime" in its name for a retired preview
	// model, so the API's refusal named a model nobody had configured.
	if p.model != "" {
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

// inputTranscriptionFromMetadata returns the session's input audio
// transcription config. Transcription is on by default for debugging and
// logging, and off when metadata["input_transcription"] is false; the model
// is metadata["input_transcription_model"], whisper-1 when unset (#2692).
func inputTranscriptionFromMetadata(metadata map[string]interface{}) *TranscriptionConfig {
	if enabled, ok := metadata["input_transcription"].(bool); ok && !enabled {
		return nil
	}
	model := defaultInputTranscriptionModel
	if m, ok := metadata["input_transcription_model"].(string); ok && m != "" {
		model = m
	}
	return &TranscriptionConfig{Model: model}
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

	config.InputAudioTranscription = inputTranscriptionFromMetadata(metadata)

	// Turn detection / VAD settings
	if vadDisabled, ok := metadata["vad_disabled"].(bool); ok && vadDisabled {
		config.TurnDetection = nil // Disable VAD for manual turn control
	}

	// Temperature
	if temp, ok := metadata["temperature"].(float64); ok {
		config.Temperature = temp
	}
}

// applyStreamSampling applies the prompt's sampling parameters to a realtime
// session. The GA session takes max_output_tokens and no sampling parameter,
// so the rest are reported as not sent.
func (p *Provider) applyStreamSampling(s *providers.StreamingSampling, config *RealtimeSessionConfig) {
	if s == nil {
		return
	}
	if s.MaxTokens > 0 {
		config.MaxResponseOutputTokens = s.MaxTokens
	}
	providers.WarnUnsentParams(p.ID(), s.Request(), providers.ParamTemperature, providers.ParamTopP,
		providers.ParamTopK, providers.ParamFrequencyPenalty, providers.ParamPresencePenalty)
}

// applyStreamTools converts and applies the request's tools and tool choice.
func (p *Provider) applyStreamTools(req *providers.StreamingInputConfig, config *RealtimeSessionConfig) {
	config.ToolChoice = req.ToolChoice
	tools := req.Tools
	if len(tools) == 0 {
		return
	}

	config.Tools = make([]RealtimeToolDefinition, len(tools))
	for i, tool := range tools {
		config.Tools[i] = RealtimeToolDefinition{
			Type:        typeFunction,
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		}
	}
	logger.Debug("CreateStreamSession: tools configured", "tool_count", len(config.Tools))
}

// SupportsStreamInput returns the media types supported for streaming input:
// audio when the platform can open a realtime session and the model is a
// realtime model (isRealtimeModel), else none.
func (p *Provider) SupportsStreamInput() []string {
	if _, err := p.realtimeDial(); err != nil {
		return []string{} // a session on this platform is refused
	}
	if p.isRealtimeModel() {
		return []string{types.ContentTypeAudio}
	}
	return []string{}
}

// realtimeConfigKey is the additional_config key that declares whether the
// model is served by the Realtime API.
const realtimeConfigKey = "realtime"

// isRealtimeModel reports whether the model is served by the Realtime API.
// additional_config.realtime (a bool) decides when it is set, so a deployment
// or proxy alias without "realtime" in its name can be declared one, and a
// name that contains it can be declared not one. Unset, or not a bool, the
// model name decides: it is a realtime model when it contains "realtime".
func (p *Provider) isRealtimeModel() bool {
	if raw, ok := p.additionalConfig[realtimeConfigKey]; ok {
		if declared, isBool := raw.(bool); isBool {
			return declared
		}
		logger.Warn("openai: ignoring non-boolean additional_config.realtime",
			"value", raw, "expected", "true|false")
	}
	return strings.Contains(p.model, "realtime")
}

// GetStreamingCapabilities returns detailed information about OpenAI's streaming support.
func (p *Provider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return RealtimeStreamingCapabilities()
}
