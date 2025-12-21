// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Realtime API constants
const (
	// RealtimeAPIEndpoint is the base WebSocket endpoint for OpenAI Realtime API.
	RealtimeAPIEndpoint = "wss://api.openai.com/v1/realtime"

	// RealtimeBetaHeader is required for the Realtime API.
	RealtimeBetaHeader = "realtime=v1"

	// Default audio configuration for OpenAI Realtime API.
	// OpenAI Realtime uses 24kHz 16-bit PCM mono audio.
	DefaultRealtimeSampleRate = 24000
	DefaultRealtimeChannels   = 1
	DefaultRealtimeBitDepth   = 16

	// Default session configuration values
	defaultTemperature       = 0.8
	defaultVADThreshold      = 0.5
	defaultPrefixPaddingMs   = 300
	defaultSilenceDurationMs = 500
)

// RealtimeSessionConfig configures a new OpenAI Realtime streaming session.
type RealtimeSessionConfig struct {
	// Model specifies the model to use (e.g., "gpt-4o-realtime-preview").
	Model string

	// Modalities specifies the input/output modalities.
	// Valid values: "text", "audio"
	// Default: ["text", "audio"]
	Modalities []string

	// Instructions is the system prompt for the session.
	Instructions string

	// Voice selects the voice for audio output.
	// Options: "alloy", "echo", "fable", "onyx", "nova", "shimmer"
	// Default: "alloy"
	Voice string

	// InputAudioFormat specifies the format for input audio.
	// Options: "pcm16", "g711_ulaw", "g711_alaw"
	// Default: "pcm16"
	InputAudioFormat string

	// OutputAudioFormat specifies the format for output audio.
	// Options: "pcm16", "g711_ulaw", "g711_alaw"
	// Default: "pcm16"
	OutputAudioFormat string

	// InputAudioTranscription configures transcription of input audio.
	// If nil, input transcription is disabled.
	InputAudioTranscription *TranscriptionConfig

	// TurnDetection configures server-side voice activity detection.
	// If nil, VAD is disabled and turn management is manual.
	TurnDetection *TurnDetectionConfig

	// Tools defines available functions for the session.
	Tools []RealtimeToolDefinition

	// Temperature controls randomness (0.6-1.2, default 0.8).
	Temperature float64

	// MaxResponseOutputTokens limits response length.
	// Use "inf" for unlimited, or a specific number.
	MaxResponseOutputTokens interface{}
}

// TranscriptionConfig configures audio transcription.
type TranscriptionConfig struct {
	// Model specifies the transcription model.
	// Default: "whisper-1"
	Model string
}

// TurnDetectionConfig configures server-side VAD.
type TurnDetectionConfig struct {
	// Type specifies the VAD type.
	// Options: "server_vad"
	Type string

	// Threshold is the activation threshold (0.0-1.0).
	// Default: 0.5
	Threshold float64

	// PrefixPaddingMs is audio padding before speech in milliseconds.
	// Default: 300
	PrefixPaddingMs int

	// SilenceDurationMs is silence duration to detect end of speech.
	// Default: 500
	SilenceDurationMs int

	// CreateResponse determines if a response is automatically created
	// when speech ends. Default: true
	CreateResponse bool
}

// RealtimeToolDefinition defines a function available in the session.
type RealtimeToolDefinition struct {
	// Type is always "function" for function tools.
	Type string `json:"type"`

	// Name is the function name.
	Name string `json:"name"`

	// Description explains what the function does.
	Description string `json:"description,omitempty"`

	// Parameters is the JSON Schema for function parameters.
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// DefaultRealtimeSessionConfig returns sensible defaults for a Realtime session.
func DefaultRealtimeSessionConfig() RealtimeSessionConfig {
	return RealtimeSessionConfig{
		Model:             "gpt-4o-realtime-preview",
		Modalities:        []string{"text", "audio"},
		Voice:             "alloy",
		InputAudioFormat:  "pcm16",
		OutputAudioFormat: "pcm16",
		Temperature:       defaultTemperature,
		TurnDetection: &TurnDetectionConfig{
			Type:              "server_vad",
			Threshold:         defaultVADThreshold,
			PrefixPaddingMs:   defaultPrefixPaddingMs,
			SilenceDurationMs: defaultSilenceDurationMs,
			CreateResponse:    true,
		},
	}
}

// RealtimeStreamingCapabilities returns the streaming capabilities for OpenAI Realtime API.
func RealtimeStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{
		SupportedMediaTypes:  []string{"audio"},
		BidirectionalSupport: true,
		MaxSessionDuration:   0, // No documented limit
		Audio: &providers.AudioStreamingCapabilities{
			SupportedEncodings:   []string{"pcm16", "g711_ulaw", "g711_alaw"},
			SupportedSampleRates: []int{24000},
			SupportedChannels:    []int{1},
			SupportedBitDepths:   []int{16},
			PreferredEncoding:    "pcm16",
			PreferredSampleRate:  DefaultRealtimeSampleRate,
		},
	}
}
