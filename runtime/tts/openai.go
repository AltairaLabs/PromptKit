package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// Compile-time checks: all three TTS service types must satisfy base.TTSProvider.
var (
	_ base.TTSProvider = (*OpenAIService)(nil)
	_ base.TTSProvider = (*ElevenLabsService)(nil)
	_ base.TTSProvider = (*CartesiaService)(nil)
)

const (
	openAIBaseURL     = "https://api.openai.com/v1"
	openAITTSEndpoint = "/audio/speech"

	// ModelTTS1 is the OpenAI TTS model optimized for speed.
	ModelTTS1 = "tts-1"
	// ModelTTS1HD is the OpenAI TTS model optimized for quality.
	ModelTTS1HD = "tts-1-hd"

	// Default timeout for TTS requests.
	defaultOpenAITimeout = 30 * time.Second

	// HTTP status code threshold for server errors.
	openAIServerErrorThreshold = 500

	// Audio format names for OpenAI.
	openAIFormatMP3  = "mp3"
	openAIFormatOpus = "opus"
	openAIFormatAAC  = "aac"
	openAIFormatFLAC = "flac"
	openAIFormatWAV  = "wav"
	openAIFormatPCM  = "pcm"

	// Audio format settings.
	openAISampleRate24 = 24000
	openAIBitDepth16   = 16

	// openAICharRatePerChar is the per-character rate for OpenAI tts-1 ($15/1M chars).
	openAICharRatePerChar = 15.0 / 1_000_000
)

// OpenAI voices.
const (
	VoiceAlloy   = "alloy"   // Neutral voice.
	VoiceEcho    = "echo"    // Male voice.
	VoiceFable   = "fable"   // British accent.
	VoiceOnyx    = "onyx"    // Deep male voice.
	VoiceNova    = "nova"    // Female voice.
	VoiceShimmer = "shimmer" // Soft female voice.
)

// openAIDefaultPricing is the inline pricing descriptor for OpenAI TTS.
// Rates: tts-1 = $15/1M chars (OpenAI public pricing). Voice is tracked as a
// dimension for forward-compat but does not differentiate rates today.
var openAIDefaultPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: "usd",
	Items:    []base.PriceItem{{Unit: "character", Rate: openAICharRatePerChar}},
}

// OpenAIService implements TTS using OpenAI's text-to-speech API.
type OpenAIService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client
}

// OpenAIOption configures the OpenAI TTS service.
// It is a type alias for base.HTTPServiceOption so callers can pass
// base.WithBaseURL, base.WithClient, base.WithModel, etc. directly.
type OpenAIOption = base.HTTPServiceOption

// NewOpenAI creates an OpenAI TTS service.
func NewOpenAI(apiKey string, opts ...OpenAIOption) *OpenAIService {
	impl, fields := base.NewHTTPService(apiKey, base.HTTPServiceDefaults{
		Name:    "openai",
		Type:    base.ProviderTypeTTS,
		Pricing: openAIDefaultPricing,
		BaseURL: openAIBaseURL,
		Model:   ModelTTS1,
		Timeout: defaultOpenAITimeout,
	}, opts...)
	return &OpenAIService{Implementation: impl, HTTPServiceFields: fields}
}

// ImplName returns the implementation name for cost tracking.
func (s *OpenAIService) ImplName() string { return "openai" }

// ModelName returns the configured model name for cost tracking.
func (s *OpenAIService) ModelName() string { return s.Model }

// openAIRequest is the request body for OpenAI TTS API.
type openAIRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

// Synthesize converts text to audio using OpenAI's TTS API.
//
//nolint:gocritic // hugeParam: SynthesisConfig passed by value to satisfy Service interface
func (s *OpenAIService) Synthesize(
	ctx context.Context, text string, config SynthesisConfig,
) (io.ReadCloser, error) {
	if text == "" {
		return nil, ErrEmptyText
	}

	// Use config voice or default
	voice := config.Voice
	if voice == "" {
		voice = VoiceAlloy
	}

	// Map format to OpenAI format string
	format := openAIFormatMP3
	if config.Format.Name != "" {
		format = s.mapFormat(config.Format)
	}

	// Speed defaults to 1.0
	speed := config.Speed
	if speed == 0 {
		speed = 1.0
	}

	// Use config model or service default
	model := config.Model
	if model == "" {
		model = s.Model
	}

	reqBody := openAIRequest{
		Model:          model,
		Input:          text,
		Voice:          voice,
		ResponseFormat: format,
		Speed:          speed,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.BaseURL+openAITTSEndpoint,
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, NewSynthesisError("openai", "", "request failed", err, true)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, s.handleError(resp)
	}

	return resp.Body, nil
}

// mapFormat converts AudioFormat to OpenAI format string.
func (s *OpenAIService) mapFormat(format AudioFormat) string {
	switch format.Name {
	case openAIFormatMP3:
		return openAIFormatMP3
	case openAIFormatOpus:
		return openAIFormatOpus
	case openAIFormatAAC:
		return openAIFormatAAC
	case openAIFormatFLAC:
		return openAIFormatFLAC
	case openAIFormatWAV:
		return openAIFormatWAV
	case openAIFormatPCM:
		return openAIFormatPCM
	default:
		return openAIFormatMP3
	}
}

// openAIErrorResponse represents an error response from OpenAI.
type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// handleError processes an error response from OpenAI.
func (s *OpenAIService) handleError(resp *http.Response) error {
	var errResp openAIErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return NewSynthesisError(
			"openai",
			fmt.Sprintf("%d", resp.StatusCode),
			"unknown error",
			err,
			resp.StatusCode >= openAIServerErrorThreshold,
		)
	}

	retryable := resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= openAIServerErrorThreshold

	var cause error
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		cause = ErrRateLimited
	case http.StatusUnauthorized:
		cause = fmt.Errorf("invalid API key")
	case http.StatusBadRequest:
		if errResp.Error.Code == "invalid_voice" {
			cause = ErrInvalidVoice
		} else {
			cause = fmt.Errorf("bad request")
		}
	}

	return NewSynthesisError(
		"openai",
		errResp.Error.Code,
		errResp.Error.Message,
		cause,
		retryable,
	)
}

// SupportedVoices returns available OpenAI voices.
func (s *OpenAIService) SupportedVoices() []Voice {
	return []Voice{
		{
			ID:          VoiceAlloy,
			Name:        "Alloy",
			Language:    "en",
			Gender:      "neutral",
			Description: "Balanced, versatile voice",
		},
		{
			ID:          VoiceEcho,
			Name:        "Echo",
			Language:    "en",
			Gender:      "male",
			Description: "Clear male voice",
		},
		{
			ID:          VoiceFable,
			Name:        "Fable",
			Language:    "en",
			Gender:      "female",
			Description: "Expressive, British accent",
		},
		{
			ID:          VoiceOnyx,
			Name:        "Onyx",
			Language:    "en",
			Gender:      "male",
			Description: "Deep, authoritative voice",
		},
		{
			ID:          VoiceNova,
			Name:        "Nova",
			Language:    "en",
			Gender:      "female",
			Description: "Warm, friendly voice",
		},
		{
			ID:          VoiceShimmer,
			Name:        "Shimmer",
			Language:    "en",
			Gender:      "female",
			Description: "Soft, calm voice",
		},
	}
}

// SupportedFormats returns audio formats supported by OpenAI TTS.
func (s *OpenAIService) SupportedFormats() []AudioFormat {
	return []AudioFormat{
		FormatMP3,
		FormatOpus,
		FormatAAC,
		FormatFLAC,
		FormatWAV,
		{
			Name:       "pcm",
			MIMEType:   "audio/pcm",
			SampleRate: openAISampleRate24,
			BitDepth:   openAIBitDepth16,
			Channels:   1,
		},
	}
}
