package tts

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tts/markup"
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
	Mode                 string                        `json:"mode"`
	ID                   string                        `json:"id,omitempty"`
	ExperimentalControls *cartesiaExperimentalControls `json:"__experimental_controls,omitempty"`
}

// cartesiaExperimentalControls carries Cartesia's experimental voice knobs.
// Currently only `Emotion` is populated, derived from markup tags in the
// input text. Other knobs (speed, etc.) live here when added.
type cartesiaExperimentalControls struct {
	// Emotion is Cartesia's emotion-array control. Each entry has the
	// form "<name>:<level>" (e.g. "positivity:high"). Cartesia silently
	// ignores unknown emotion names, so we map a conservative subset of
	// our canonical tags and drop the rest.
	Emotion []string `json:"emotion,omitempty"`
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

	voice := config.Voice
	if voice == "" {
		voice = cartesiaDefaultVoice
	}
	model := config.Model
	if model == "" {
		model = s.Model
	}

	// Lower markup tags into Cartesia's dialect: bracket directives are
	// translated to entries in the experimental-controls `emotion` array,
	// and stripped from the transcript so they are not spoken literally.
	// Plain text (no tags) produces a request that's byte-identical to
	// the pre-markup wire format, keeping the audio cache key stable.
	transcript, emotions := lowerCartesiaMarkup(text)

	voiceCfg := cartesiaVoiceConfig{Mode: "id", ID: voice}
	if len(emotions) > 0 {
		voiceCfg.ExperimentalControls = &cartesiaExperimentalControls{Emotion: emotions}
	}

	reqBody := cartesiaRequest{
		ModelID:      model,
		Transcript:   transcript,
		Voice:        voiceCfg,
		OutputFormat: s.mapFormat(config.Format),
		Language:     config.Language,
	}

	headers := map[string]string{
		"X-API-Key":        s.APIKey,
		"Content-Type":     "application/json",
		"Cartesia-Version": "2024-06-10",
	}
	return postJSONForAudio(
		ctx, s.Client, "cartesia", s.BaseURL+cartesiaRESTURL, reqBody, headers, s.handleError,
	)
}

// lowerCartesiaMarkup translates characterization tags in text into the
// Cartesia request shape: a stripped transcript and a slice of "emotion:level"
// strings suitable for the experimental-controls emotion array. Returns the
// raw text and a nil slice when no tags are present (cache key stable).
//
// The taxonomy mapping is conservative — Cartesia's emotion vocabulary is
// narrow (positivity / sadness / anger / surprise / curiosity) so several
// of our canonical tag names (whispers, pause, calm) have no analog and
// are dropped. Cartesia silently ignores unrecognized emotion entries, so
// the mapping degrades safely even if Cartesia's API changes.
func lowerCartesiaMarkup(text string) (transcript string, emotions []string) {
	tags := markup.ParseTags(text)
	if len(tags) == 0 {
		return text, nil
	}
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		if t.IsClose() {
			continue
		}
		e := cartesiaEmotionForTag(t)
		if e == "" {
			continue
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		emotions = append(emotions, e)
	}
	return markup.StripTags(text), emotions
}

// cartesiaEmotionForTag maps a canonical tag onto Cartesia's experimental
// emotion vocabulary, or returns "" when there is no sensible analog (the
// tag is then dropped). Levels follow Cartesia's "<emotion>:<level>" form
// with levels lowest/low/medium/high/highest.
func cartesiaEmotionForTag(t markup.Tag) string {
	switch t.Name {
	case "excited", "laughs", "laugh":
		return "positivity:high"
	case "smile", "smiles":
		return "positivity:medium"
	case "sad", "sighs", "sigh":
		return "sadness:high"
	case "shouts", "shout":
		return "anger:high"
	default:
		// whispers/calm/pause and unknown names — no Cartesia analog.
		return ""
	}
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
	if e := decodeErrorBody("cartesia", resp, &errResp); e != nil {
		return e
	}
	retryable, cause := classifyHTTPStatus(resp.StatusCode, ErrInvalidVoice, fmt.Errorf("bad request"))
	message := errResp.Message
	if message == "" {
		message = errResp.Error
	}
	return NewSynthesisError("cartesia", errResp.Error, message, cause, retryable)
}

// SupportedVoices returns a sample of available Cartesia voices.
func (s *CartesiaService) SupportedVoices() []Voice {
	return []Voice{
		makeVoice("a0e99841-438c-4a64-b679-ae501e7d6091", "Barbershop Man", "en", "male", "Deep, warm male voice"),
		makeVoice("156fb8d2-335b-4950-9cb3-a2d33befec77", "British Lady", "en", "female", "British accent, professional"),
		makeVoice("79a125e8-cd45-4c13-8a67-188112f4dd22", "California Girl", "en", "female", "Casual, friendly American"),
		makeVoice("bf991597-6c13-47e4-8411-91ec2de5c466", "Confident Man", "en", "male", "Clear, confident delivery"),
		makeVoice("9121c0ae-12a6-4012-8158-6e4a72e6da91", "Friendly Woman", "en", "female", "Warm, approachable"),
	}
}

// SupportedFormats returns audio formats supported by Cartesia.
func (s *CartesiaService) SupportedFormats() []AudioFormat {
	return []AudioFormat{
		makeFormat(formatPCM, "audio/pcm", sampleRate24000, bitDepth16),
		makeFormat(formatMP3, "audio/mpeg", sampleRate44100, 0),
		makeFormat(formatWAV, "audio/wav", sampleRate44100, bitDepth16),
	}
}
