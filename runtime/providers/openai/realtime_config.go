// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"fmt"
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// GA Realtime session literal values, named to avoid repeated string literals.
const (
	sessionTypeRealtime = "realtime"
	mimeAudioPCM        = "audio/pcm"
)

// buildRealtimeSessionConfig converts our internal RealtimeSessionConfig into
// the GA Realtime session.update payload. Output modalities flatten (audio
// implies text-too); codec, voice, VAD, and transcription nest under
// audio.{input,output}; voice moved into audio.output.voice.
//
// When TurnDetection is nil we leave audio.input.turn_detection nil; the
// pointer-without-omitempty tag marshals it as `"turn_detection":null` — the GA
// signal for "manual turn control".
func buildRealtimeSessionConfig(config RealtimeSessionConfig) SessionConfig {
	cfg := SessionConfig{
		Type:             sessionTypeRealtime,
		Instructions:     config.Instructions,
		OutputModalities: outputModalities(config.Modalities),
		Audio: &RealtimeAudioConfig{
			Input: &RealtimeAudioInput{
				Format: pcmFormat(config.InputAudioFormat, DefaultRealtimeSampleRate),
			},
			Output: &RealtimeAudioOutput{
				Format: pcmFormat(config.OutputAudioFormat, DefaultRealtimeSampleRate),
				Voice:  config.Voice,
			},
		},
	}

	if config.InputAudioTranscription != nil {
		cfg.Audio.Input.Transcription = &TranscriptionConfig{
			Model: config.InputAudioTranscription.Model,
		}
	}

	if config.TurnDetection != nil {
		cfg.Audio.Input.TurnDetection = &TurnDetectionConfig{
			Type:              config.TurnDetection.Type,
			Threshold:         config.TurnDetection.Threshold,
			PrefixPaddingMs:   config.TurnDetection.PrefixPaddingMs,
			SilenceDurationMs: config.TurnDetection.SilenceDurationMs,
			CreateResponse:    config.TurnDetection.CreateResponse,
		}
	}

	if len(config.Tools) > 0 {
		cfg.Tools = make([]RealtimeToolDef, len(config.Tools))
		for i, tool := range config.Tools {
			cfg.Tools[i] = RealtimeToolDef{
				Type:        typeFunction,
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			}
		}
	}

	cfg.ToolChoice = realtimeToolChoice(config.ToolChoice, cfg.Tools)

	if config.MaxResponseOutputTokens != nil {
		cfg.MaxOutputTokens = config.MaxResponseOutputTokens
	}

	return cfg
}

// realtimeToolChoice is the GA tool_choice for choice over tools: a mode
// ("auto", "required", "none") as is, any other name as a function choice. It
// is nil (omitted) when choice is empty, when there are no tools (a session
// requiring a tool it lacks is rejected by the server), and when choice names
// a function tools lacks.
func realtimeToolChoice(choice string, tools []RealtimeToolDef) interface{} {
	if choice == "" || len(tools) == 0 {
		return nil
	}
	switch choice {
	case toolChoiceAuto, toolChoiceRequired, toolChoiceNone:
		return choice
	}
	for i := range tools {
		if tools[i].Name == choice {
			return map[string]string{"type": typeFunction, "name": choice}
		}
	}
	return nil
}

// realtimeToolDefs converts streaming tool definitions to the session's tool
// format. The result is never nil, so an empty list marshals as [].
func realtimeToolDefs(tools []providers.StreamingToolDefinition) []RealtimeToolDef {
	out := make([]RealtimeToolDef, len(tools))
	for i, tool := range tools {
		out[i] = RealtimeToolDef{
			Type:        typeFunction,
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		}
	}
	return out
}

// Realtime modality names shared with the GA output_modalities field.
const (
	modalityAudio = "audio"
	modalityText  = "text"
)

// outputModalities maps our internal modality list onto the GA
// output_modalities field. The GA API accepts ["audio"] (which implies
// text-too) or ["text"] for text-only.
func outputModalities(in []string) []string {
	for _, m := range in {
		if m == modalityAudio {
			return []string{modalityAudio}
		}
	}
	if len(in) == 0 {
		return []string{modalityAudio} // safe default for a Realtime session
	}
	return []string{modalityText}
}

// pcmFormat returns the GA-shape audio format descriptor for a legacy
// "pcm16"-style codec name. Empty / unknown formats fall through as nil
// so the server uses its default.
func pcmFormat(legacy string, rate int) *RealtimeAudioFormat {
	if legacy != "" && legacy != audioPCM16Format {
		return nil
	}
	return &RealtimeAudioFormat{Type: mimeAudioPCM, Rate: rate}
}

// markToolCallEmitted records that a tool call for the given item ID was already
// emitted to responseCh; returns true if the caller should skip its emission.
// Items with empty IDs are never deduped (best-effort emit).
func (s *RealtimeSession) markToolCallEmitted(itemID string) bool {
	if itemID == "" {
		return false
	}
	_, alreadyEmitted := s.emittedToolCalls.LoadOrStore(itemID, struct{}{})
	return alreadyEmitted
}

// realtimeWSURL builds the Realtime WebSocket URL, carrying the model as a query
// parameter (the GA API selects the model via ?model=).
func realtimeWSURL(endpoint, model string) string {
	return fmt.Sprintf("%s?model=%s", endpoint, model)
}

// realtimeWSHeaders builds the HTTP headers for the Realtime WebSocket
// handshake. It sets Authorization but deliberately does NOT set the
// OpenAI-Beta header: per OpenAI's GA migration guide the legacy beta header is
// rejected by gpt-realtime and friends (the server treats it as a legacy
// connection and serves pre-GA defaults that conflict with the current event
// schema).
func realtimeWSHeaders(apiKey string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	return headers
}
