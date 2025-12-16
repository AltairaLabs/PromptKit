package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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

	// HTTP status code threshold for server errors.
	elevenLabsServerErrorThreshold = 500

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
)

// ElevenLabsService implements TTS using ElevenLabs' API.
// ElevenLabs specializes in high-quality voice cloning and natural-sounding speech.
type ElevenLabsService struct {
	apiKey  string
	baseURL string
	client  *http.Client
	model   string
}

// ElevenLabsOption configures the ElevenLabs TTS service.
type ElevenLabsOption func(*ElevenLabsService)

// WithElevenLabsBaseURL sets a custom base URL.
func WithElevenLabsBaseURL(url string) ElevenLabsOption {
	return func(s *ElevenLabsService) {
		s.baseURL = url
	}
}

// WithElevenLabsClient sets a custom HTTP client.
func WithElevenLabsClient(client *http.Client) ElevenLabsOption {
	return func(s *ElevenLabsService) {
		s.client = client
	}
}

// WithElevenLabsModel sets the TTS model.
func WithElevenLabsModel(model string) ElevenLabsOption {
	return func(s *ElevenLabsService) {
		s.model = model
	}
}

// NewElevenLabs creates an ElevenLabs TTS service.
func NewElevenLabs(apiKey string, opts ...ElevenLabsOption) *ElevenLabsService {
	s := &ElevenLabsService{
		apiKey:  apiKey,
		baseURL: elevenLabsBaseURL,
		client:  &http.Client{Timeout: defaultElevenLabsTimeout},
		model:   ElevenLabsModelMultilingual,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name returns the provider identifier.
func (s *ElevenLabsService) Name() string {
	return "elevenlabs"
}

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

	// Use config voice or default
	voice := config.Voice
	if voice == "" {
		voice = elevenLabsDefaultVoice
	}

	// Use config model or service default
	model := config.Model
	if model == "" {
		model = s.model
	}

	reqBody := elevenLabsRequest{
		Text:    text,
		ModelID: model,
		VoiceSettings: &elevenLabsVoiceSettings{
			Stability:       elevenLabsDefaultStability,
			SimilarityBoost: elevenLabsDefaultSimilarityBoost,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/text-to-speech/%s", s.baseURL, voice)

	// Add output format query parameter
	format := s.mapFormat(config.Format)
	if format != "" {
		endpoint += "?output_format=" + format
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("xi-api-key", s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, NewSynthesisError("elevenlabs", "", "request failed", err, true)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, s.handleError(resp)
	}

	return resp.Body, nil
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
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return NewSynthesisError(
			"elevenlabs",
			fmt.Sprintf("%d", resp.StatusCode),
			"unknown error",
			err,
			resp.StatusCode >= elevenLabsServerErrorThreshold,
		)
	}

	retryable := resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= elevenLabsServerErrorThreshold

	var cause error
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		cause = ErrRateLimited
	case http.StatusUnauthorized:
		cause = fmt.Errorf("invalid API key")
	case http.StatusBadRequest:
		cause = fmt.Errorf("bad request")
	case http.StatusNotFound:
		cause = ErrInvalidVoice
	}

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
		{
			ID:          "21m00Tcm4TlvDq8ikWAM",
			Name:        "Rachel",
			Language:    "en",
			Gender:      "female",
			Description: "American, calm, young",
		},
		{
			ID:          "AZnzlk1XvdvUeBnXmlld",
			Name:        "Domi",
			Language:    "en",
			Gender:      "female",
			Description: "American, confident",
		},
		{
			ID:          "EXAVITQu4vr4xnSDxMaL",
			Name:        "Bella",
			Language:    "en",
			Gender:      "female",
			Description: "American, soft",
		},
		{
			ID:          "ErXwobaYiN019PkySvjV",
			Name:        "Antoni",
			Language:    "en",
			Gender:      "male",
			Description: "American, well-rounded",
		},
		{
			ID:          "MF3mGyEYCl7XYWbV9V6O",
			Name:        "Elli",
			Language:    "en",
			Gender:      "female",
			Description: "American, young",
		},
		{
			ID:          "TxGEqnHWrfWFTfGW9XjX",
			Name:        "Josh",
			Language:    "en",
			Gender:      "male",
			Description: "American, young, deep",
		},
		{
			ID:          "VR6AewLTigWG4xSOukaG",
			Name:        "Arnold",
			Language:    "en",
			Gender:      "male",
			Description: "American, deep, professional",
		},
		{
			ID:          "pNInz6obpgDQGcFmaJgB",
			Name:        "Adam",
			Language:    "en",
			Gender:      "male",
			Description: "American, deep, narrative",
		},
		{
			ID:          "yoZ06aMxZJJ28mfd3POQ",
			Name:        "Sam",
			Language:    "en",
			Gender:      "male",
			Description: "American, young, casual",
		},
	}
}

// SupportedFormats returns audio formats supported by ElevenLabs.
func (s *ElevenLabsService) SupportedFormats() []AudioFormat {
	return []AudioFormat{
		{
			Name:       "mp3",
			MIMEType:   "audio/mpeg",
			SampleRate: elevenLabsSampleRate44,
			BitDepth:   0,
			Channels:   1,
		},
		{
			Name:       "pcm",
			MIMEType:   "audio/pcm",
			SampleRate: elevenLabsSampleRate24,
			BitDepth:   elevenLabsBitDepth16,
			Channels:   1,
		},
	}
}
