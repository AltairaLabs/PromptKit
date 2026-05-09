package tts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

const (
	elevenLabsBaseURL = "https://api.elevenlabs.io/v1"

	// ElevenLabsModelMultilingual is the multilingual v2 model.
	ElevenLabsModelMultilingual = "eleven_multilingual_v2"
	// ElevenLabsModelTurbo is the fast turbo v2.5 model.
	ElevenLabsModelTurbo = "eleven_turbo_v2_5"
	// ElevenLabsModelEnglish is the English monolingual v1 model.
	ElevenLabsModelEnglish = "eleven_monolingual_v1"
	// ElevenLabsModelMultilingualV1 is the older multilingual v1 model.
	ElevenLabsModelMultilingualV1 = "eleven_multilingual_v1"

	// Default timeout for ElevenLabs requests.
	defaultElevenLabsTimeout = 60 * time.Second

	// Default voice settings.
	elevenLabsDefaultStability       = 0.5
	elevenLabsDefaultSimilarityBoost = 0.75

	// Default voice ID (Rachel).
	elevenLabsDefaultVoice = "21m00Tcm4TlvDq8ikWAM"

	// Audio format constants.
	elevenLabsFormatMP3    = "mp3_44100_128"
	elevenLabsFormatPCM    = "pcm_24000"
	elevenLabsBitDepth16   = 16
	elevenLabsSampleRate44 = 44100
	elevenLabsSampleRate24 = 24000

	// elevenLabsCharRatePerChar is the per-character rate for ElevenLabs TTS.
	// Creator-tier: ~$180/1M chars (conservative hardcoded estimate).
	elevenLabsCharRatePerChar = 180.0 / 1_000_000
)

// elevenLabsDefaultPricing is the inline pricing descriptor for ElevenLabs TTS.
// Rate: Creator-tier ~$180/1M chars (hardcoded conservative estimate).
var elevenLabsDefaultPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: "usd",
	Items:    []base.PriceItem{{Unit: "character", Rate: elevenLabsCharRatePerChar}},
}

// ElevenLabsService implements TTS using ElevenLabs' API.
// ElevenLabs specializes in high-quality voice cloning and natural-sounding speech.
type ElevenLabsService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client
}

// ElevenLabsOption configures the ElevenLabs TTS service.
// It is a type alias for base.HTTPServiceOption so callers can pass
// base.WithBaseURL, base.WithClient, base.WithModel, etc. directly.
type ElevenLabsOption = base.HTTPServiceOption

// NewElevenLabs creates an ElevenLabs TTS service.
func NewElevenLabs(apiKey string, opts ...ElevenLabsOption) *ElevenLabsService {
	impl, fields := base.NewHTTPService(apiKey, base.HTTPServiceDefaults{
		Name:    "elevenlabs",
		Type:    base.ProviderTypeTTS,
		Pricing: elevenLabsDefaultPricing,
		BaseURL: elevenLabsBaseURL,
		Model:   ElevenLabsModelMultilingual,
		Timeout: defaultElevenLabsTimeout,
	}, opts...)
	return &ElevenLabsService{Implementation: impl, HTTPServiceFields: fields}
}

// ImplName returns the implementation name for cost tracking.
func (s *ElevenLabsService) ImplName() string { return "elevenlabs" }

// ModelName returns the configured model name for cost tracking.
func (s *ElevenLabsService) ModelName() string { return s.Model }

// elevenLabsRequest is the request body for ElevenLabs TTS API.
type elevenLabsRequest struct {
	Text          string                   `json:"text"`
	ModelID       string                   `json:"model_id,omitempty"`
	VoiceSettings *elevenLabsVoiceSettings `json:"voice_settings,omitempty"`
}

// elevenLabsVoiceSettings configures voice parameters.
type elevenLabsVoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Style           float64 `json:"style,omitempty"`
	UseSpeakerBoost bool    `json:"use_speaker_boost,omitempty"`
}

// Synthesize converts text to audio using ElevenLabs' TTS API.
//
//nolint:gocritic // hugeParam: SynthesisConfig passed by value to satisfy Service interface
func (s *ElevenLabsService) Synthesize(
	ctx context.Context, text string, config SynthesisConfig,
) (io.ReadCloser, error) {
	if text == "" {
		return nil, ErrEmptyText
	}

	voice := config.Voice
	if voice == "" {
		voice = elevenLabsDefaultVoice
	}
	model := config.Model
	if model == "" {
		model = s.Model
	}

	reqBody := elevenLabsRequest{
		Text:    text,
		ModelID: model,
		VoiceSettings: &elevenLabsVoiceSettings{
			Stability:       elevenLabsDefaultStability,
			SimilarityBoost: elevenLabsDefaultSimilarityBoost,
		},
	}

	endpoint := fmt.Sprintf("%s/text-to-speech/%s", s.BaseURL, voice)
	if format := s.mapFormat(config.Format); format != "" {
		endpoint += "?output_format=" + format
	}

	headers := map[string]string{
		"xi-api-key":   s.APIKey,
		"Content-Type": "application/json",
		"Accept":       "audio/mpeg",
	}
	return postJSONForAudio(ctx, s.Client, "elevenlabs", endpoint, reqBody, headers, s.handleError)
}

// mapFormat converts AudioFormat to ElevenLabs format string.
func (s *ElevenLabsService) mapFormat(format AudioFormat) string {
	switch format.Name {
	case "mp3":
		return elevenLabsFormatMP3
	case "pcm":
		return elevenLabsFormatPCM
	case "":
		return elevenLabsFormatMP3
	default:
		return elevenLabsFormatMP3
	}
}

// elevenLabsErrorResponse represents an error response from ElevenLabs.
type elevenLabsErrorResponse struct {
	Detail struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"detail"`
}

// handleError processes an error response from ElevenLabs.
func (s *ElevenLabsService) handleError(resp *http.Response) error {
	var errResp elevenLabsErrorResponse
	if e := decodeErrorBody("elevenlabs", resp, &errResp); e != nil {
		return e
	}
	retryable, cause := classifyHTTPStatus(resp.StatusCode, ErrInvalidVoice, fmt.Errorf("bad request"))
	return NewSynthesisError(
		"elevenlabs",
		errResp.Detail.Status,
		errResp.Detail.Message,
		cause,
		retryable,
	)
}

// SupportedVoices returns a sample of available ElevenLabs voices.
// Note: ElevenLabs has many more voices including custom cloned voices.
// Use the ElevenLabs API to get a complete list of available voices.
func (s *ElevenLabsService) SupportedVoices() []Voice {
	return []Voice{
		makeVoice("21m00Tcm4TlvDq8ikWAM", "Rachel", "en", "female", "American, calm, young"),
		makeVoice("AZnzlk1XvdvUeBnXmlld", "Domi", "en", "female", "American, confident"),
		makeVoice("EXAVITQu4vr4xnSDxMaL", "Bella", "en", "female", "American, soft"),
		makeVoice("ErXwobaYiN019PkySvjV", "Antoni", "en", "male", "American, well-rounded"),
		makeVoice("MF3mGyEYCl7XYWbV9V6O", "Elli", "en", "female", "American, young"),
		makeVoice("TxGEqnHWrfWFTfGW9XjX", "Josh", "en", "male", "American, young, deep"),
		makeVoice("VR6AewLTigWG4xSOukaG", "Arnold", "en", "male", "American, deep, professional"),
		makeVoice("pNInz6obpgDQGcFmaJgB", "Adam", "en", "male", "American, deep, narrative"),
		makeVoice("yoZ06aMxZJJ28mfd3POQ", "Sam", "en", "male", "American, young, casual"),
	}
}

// SupportedFormats returns audio formats supported by ElevenLabs.
func (s *ElevenLabsService) SupportedFormats() []AudioFormat {
	return []AudioFormat{
		makeFormat("mp3", "audio/mpeg", elevenLabsSampleRate44, 0),
		makeFormat("pcm", "audio/pcm", elevenLabsSampleRate24, elevenLabsBitDepth16),
	}
}
