package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

const (
	cartesiaBaseURL = "https://api.cartesia.ai"
	cartesiaWSURL   = "wss://api.cartesia.ai/tts/websocket"
	cartesiaRESTURL = "/tts/bytes"

	// CartesiaModelSonic is the current Sonic model alias for Cartesia
	// TTS. Cartesia retired the dated `sonic-YYYY-MM-DD` IDs; "sonic-3"
	// is the latest generation (better naturalness + faster TTFB than
	// sonic-2 and the original sonic).
	CartesiaModelSonic = "sonic-3"

	// Default timeout for Cartesia requests.
	defaultCartesiaTimeout = 30 * time.Second

	// cartesiaDefaultVoice is the default voice ID (Barbershop Man).
	cartesiaDefaultVoice = "a0e99841-438c-4a64-b679-ae501e7d6091"

	// streamChannelBuffer is the buffer size for streaming audio chunks.
	streamChannelBuffer = 64

	// HTTP status code threshold for server errors.
	serverErrorThreshold = 500

	// Audio sample rates.
	sampleRate24000 = 24000
	sampleRate44100 = 44100
	bitDepth16      = 16

	// Audio format names.
	formatPCM = "pcm"
	formatMP3 = "mp3"
	formatWAV = "wav"

	// cartesiaCharRatePerChar is the per-character rate for Cartesia Sonic ($15/1M chars).
	cartesiaCharRatePerChar = 15.0 / 1_000_000
)

// cartesiaDefaultPricing is the inline pricing descriptor for Cartesia TTS.
// Rate: $15/1M chars (Cartesia's published per-character rate for Sonic).
var cartesiaDefaultPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: "usd",
	Items:    []base.PriceItem{{Unit: "character", Rate: cartesiaCharRatePerChar}},
}

// CartesiaService implements TTS using Cartesia's ultra-low latency API.
// Cartesia specializes in real-time streaming TTS with <100ms first-byte latency.
type CartesiaService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client
	wsURL                   string
}

// CartesiaOption configures the Cartesia TTS service.
// It is a type alias for base.HTTPServiceOption so callers can pass
// base.WithBaseURL, base.WithClient, base.WithModel, etc. directly.
// Use WithCartesiaWSURL for Cartesia-specific options.
type CartesiaOption = base.HTTPServiceOption

// WithCartesiaWSURL sets a custom WebSocket URL.
func WithCartesiaWSURL(url string) func(*CartesiaService) {
	return func(s *CartesiaService) {
		s.wsURL = url
	}
}

// NewCartesia creates a Cartesia TTS service.
func NewCartesia(apiKey string, opts ...CartesiaOption) *CartesiaService {
	impl, fields := base.NewHTTPService(apiKey, base.HTTPServiceDefaults{
		Name:    "cartesia",
		Type:    base.ProviderTypeTTS,
		Pricing: cartesiaDefaultPricing,
		BaseURL: cartesiaBaseURL,
		Model:   CartesiaModelSonic,
		Timeout: defaultCartesiaTimeout,
	}, opts...)
	return &CartesiaService{
		Implementation:    impl,
		HTTPServiceFields: fields,
		wsURL:             cartesiaWSURL,
	}
}

// ImplName returns the implementation name for cost tracking.
func (s *CartesiaService) ImplName() string { return "cartesia" }

// ModelName returns the configured model name for cost tracking.
func (s *CartesiaService) ModelName() string { return s.Model }

// cartesiaRequest is the request body for Cartesia TTS API.
type cartesiaRequest struct {
	ModelID       string               `json:"model_id"`
	Transcript    string               `json:"transcript"`
	Voice         cartesiaVoiceConfig  `json:"voice"`
	OutputFormat  cartesiaOutputFormat `json:"output_format"`
	Language      string               `json:"language,omitempty"`
	Duration      *float64             `json:"duration,omitempty"`
	AddTimestamps bool                 `json:"add_timestamps,omitempty"`
}

type cartesiaVoiceConfig struct {
	Mode string `json:"mode"`
	ID   string `json:"id,omitempty"`
}

type cartesiaOutputFormat struct {
	Container  string `json:"container"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

// Synthesize converts text to audio using Cartesia's REST API.
// For streaming output, use SynthesizeStream instead.
//
//nolint:gocritic // hugeParam: SynthesisConfig passed by value to satisfy Service interface
func (s *CartesiaService) Synthesize(
	ctx context.Context, text string, config SynthesisConfig,
) (io.ReadCloser, error) {
	if text == "" {
		return nil, ErrEmptyText
	}

	// Use config voice or default
	voice := config.Voice
	if voice == "" {
		voice = cartesiaDefaultVoice
	}

	// Use config model or service default
	model := config.Model
	if model == "" {
		model = s.Model
	}

	outputFormat := s.mapFormat(config.Format)

	reqBody := cartesiaRequest{
		ModelID:    model,
		Transcript: text,
		Voice: cartesiaVoiceConfig{
			Mode: "id",
			ID:   voice,
		},
		OutputFormat: outputFormat,
		Language:     config.Language,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.BaseURL+cartesiaRESTURL,
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", s.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cartesia-Version", "2024-06-10")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, NewSynthesisError("cartesia", "", "request failed", err, true)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, s.handleError(resp)
	}

	return resp.Body, nil
}

// cartesiaWSResponse represents a WebSocket response from Cartesia.
type cartesiaWSResponse struct {
	StatusCode int    `json:"status_code"`
	Done       bool   `json:"done"`
	Type       string `json:"type"`
	Data       string `json:"data"` // Base64-encoded audio
	Error      string `json:"error,omitempty"`
}

// processWSResponse processes a single WebSocket response and returns the audio chunk.
// Returns nil chunk if the response doesn't contain audio data.
// Returns error if processing fails or response contains an error.
func (s *CartesiaService) processWSResponse(
	resp *cartesiaWSResponse, index int,
) (*audio.Chunk, error) {
	if resp.Error != "" {
		return nil, NewSynthesisError("cartesia", "", resp.Error, nil, false)
	}

	if resp.Type != "chunk" || resp.Data == "" {
		return nil, nil
	}

	audioData, err := base64.StdEncoding.DecodeString(resp.Data)
	if err != nil {
		return nil, err
	}

	return &audio.Chunk{
		Data:  audioData,
		Index: index,
		Final: resp.Done,
	}, nil
}

// mapFormat converts AudioFormat to Cartesia format config.
func (s *CartesiaService) mapFormat(format AudioFormat) cartesiaOutputFormat {
	switch format.Name {
	case formatMP3:
		return cartesiaOutputFormat{
			Container:  formatMP3,
			Encoding:   formatMP3,
			SampleRate: sampleRate44100,
		}
	case formatWAV:
		return cartesiaOutputFormat{
			Container:  formatWAV,
			Encoding:   "pcm_s16le",
			SampleRate: sampleRate44100,
		}
	default:
		// Default to PCM raw format (also handles formatPCM explicitly)
		return cartesiaOutputFormat{
			Container:  "raw",
			Encoding:   "pcm_s16le",
			SampleRate: sampleRate24000,
		}
	}
}

// cartesiaErrorResponse represents an error response from Cartesia.
type cartesiaErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// handleError processes an error response from Cartesia.
func (s *CartesiaService) handleError(resp *http.Response) error {
	var errResp cartesiaErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return NewSynthesisError(
			"cartesia",
			fmt.Sprintf("%d", resp.StatusCode),
			"unknown error",
			err,
			resp.StatusCode >= serverErrorThreshold,
		)
	}

	retryable := resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= serverErrorThreshold

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

	message := errResp.Message
	if message == "" {
		message = errResp.Error
	}

	return NewSynthesisError(
		"cartesia",
		errResp.Error,
		message,
		cause,
		retryable,
	)
}

// SupportedVoices returns a sample of available Cartesia voices.
func (s *CartesiaService) SupportedVoices() []Voice {
	return []Voice{
		{
			ID:          "a0e99841-438c-4a64-b679-ae501e7d6091",
			Name:        "Barbershop Man",
			Language:    "en",
			Gender:      "male",
			Description: "Deep, warm male voice",
		},
		{
			ID:          "156fb8d2-335b-4950-9cb3-a2d33befec77",
			Name:        "British Lady",
			Language:    "en",
			Gender:      "female",
			Description: "British accent, professional",
		},
		{
			ID:          "79a125e8-cd45-4c13-8a67-188112f4dd22",
			Name:        "California Girl",
			Language:    "en",
			Gender:      "female",
			Description: "Casual, friendly American",
		},
		{
			ID:          "bf991597-6c13-47e4-8411-91ec2de5c466",
			Name:        "Confident Man",
			Language:    "en",
			Gender:      "male",
			Description: "Clear, confident delivery",
		},
		{
			ID:          "9121c0ae-12a6-4012-8158-6e4a72e6da91",
			Name:        "Friendly Woman",
			Language:    "en",
			Gender:      "female",
			Description: "Warm, approachable",
		},
	}
}

// SupportedFormats returns audio formats supported by Cartesia.
func (s *CartesiaService) SupportedFormats() []AudioFormat {
	return []AudioFormat{
		{
			Name:       formatPCM,
			MIMEType:   "audio/pcm",
			SampleRate: sampleRate24000,
			BitDepth:   bitDepth16,
			Channels:   1,
		},
		{
			Name:       formatMP3,
			MIMEType:   "audio/mpeg",
			SampleRate: sampleRate44100,
			BitDepth:   0,
			Channels:   1,
		},
		{
			Name:       formatWAV,
			MIMEType:   "audio/wav",
			SampleRate: sampleRate44100,
			BitDepth:   bitDepth16,
			Channels:   1,
		},
	}
}
