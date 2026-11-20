package tts

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts/markup"
)

const (
	cartesiaBaseURL = "https://api.cartesia.ai"
	cartesiaWSURL   = "wss://api.cartesia.ai/tts/websocket"

	// cartesiaAPIKeyHeader carries the credential. Preferred over a ?api_key=
	// query parameter so the key never enters a URL, and therefore never enters
	// a dial error or the logs that format one.
	cartesiaAPIKeyHeader = "X-API-Key" //nolint:gosec // G101: header name, not a secret

	// cartesiaAPIVersion is the wire version pinned on every request. Not a
	// secret, so it stays in the query string.
	cartesiaAPIVersion = "2024-06-10"
	cartesiaRESTURL    = "/tts/bytes"

	// CartesiaModelSonic is the Sonic model ID that CartesiaService uses by default.
	CartesiaModelSonic = "sonic-3"

	// Default timeout for Cartesia requests.
	defaultCartesiaTimeout = 30 * time.Second

	// cartesiaDefaultVoice is the voice used when SynthesisConfig.Voice is
	// empty: "Katie - Friendly Fixer", a public, active, non-pro voice with a
	// native general-American accent, built for conversational agents. Checked
	// live against GET /voices/{id} and POST /tts/bytes (#2822). The previous
	// default (Barbershop Man, a0e99841-…) was withdrawn from the public library
	// and now returns 404 voice_not_found.
	cartesiaDefaultVoice = "f786b574-daa5-4673-aa0c-cbe3e8534c02"

	// tagShouts / tagShout are the markup tag names that map to anger +
	// volume bump on Cartesia. Both spellings tolerated because the
	// rubric documents "[shouts]" but the LLM occasionally emits the
	// singular form.
	tagShouts = "shouts"
	tagShout  = "shout"

	// logPreviewLen caps the transcript-preview string in our log lines
	// so multi-sentence utterances don't flood the log.
	logPreviewLen = 80

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
	Currency: currencyUSD,
	Items:    []base.PriceItem{{Unit: unitCharacter, Rate: cartesiaCharRatePerChar}},
}

// CartesiaService implements TTS using Cartesia's ultra-low latency API.
// It supports REST synthesis (Synthesize) and WebSocket streaming (SynthesizeStream).
type CartesiaService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client
	wsURL                   string
	// wsHeaders are custom headers from HTTPTuning, sent on the websocket
	// dial as well as on REST requests.
	wsHeaders map[string]string
}

// ApplyHTTPTuning tunes the REST client and keeps the custom headers for the
// streaming websocket dial. The websocket keeps its own read deadline; the
// request timeout and transport apply to REST calls only.
func (s *CartesiaService) ApplyHTTPTuning(t base.HTTPTuning) error {
	if err := s.HTTPServiceFields.ApplyHTTPTuning(t); err != nil {
		return err
	}
	if len(t.Headers) > 0 {
		s.wsHeaders = make(map[string]string, len(t.Headers))
		for k, v := range t.Headers {
			s.wsHeaders[k] = v
		}
	}
	return nil
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
		Name:    providerCartesia,
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
func (s *CartesiaService) ImplName() string { return providerCartesia }

// ModelName returns the configured model name for cost tracking.
func (s *CartesiaService) ModelName() string { return s.Model }

// PersonaRubric implements [PersonaRubricProvider]. Returns the
// emotion-only rubric: the adapter maps only emotion tags (excited, content,
// sad, angry) onto Cartesia's generation config, so only those are advertised.
// Other tags (e.g. whispers, pause) are dropped from the request, so the
// rubric omits them.
func (s *CartesiaService) PersonaRubric() string {
	return markup.RubricEmotionOnly
}

// cartesiaRequest is the request body for Cartesia TTS API.
type cartesiaRequest struct {
	ModelID          string                    `json:"model_id"`
	Transcript       string                    `json:"transcript"`
	Voice            cartesiaVoiceConfig       `json:"voice"`
	OutputFormat     cartesiaOutputFormat      `json:"output_format"`
	Language         string                    `json:"language,omitempty"`
	Duration         *float64                  `json:"duration,omitempty"`
	AddTimestamps    bool                      `json:"add_timestamps,omitempty"`
	GenerationConfig *cartesiaGenerationConfig `json:"generation_config,omitempty"`
}

type cartesiaVoiceConfig struct {
	Mode string `json:"mode"`
	ID   string `json:"id,omitempty"`
}

// cartesiaGenerationConfig carries Cartesia's sonic-3 generation controls.
// Replaces the legacy `voice.__experimental_controls` envelope, which
// sonic-2+ models silently ignore. Documented at
// https://docs.cartesia.ai/build-with-cartesia/sonic-3/volume-speed-emotion
type cartesiaGenerationConfig struct {
	// Emotion is a single canonical name (e.g. "angry", "sad", "excited").
	// Sonic-3 accepts a fixed core set plus ~60 broader names; we only
	// emit the conservative core mapping so the request is robust to
	// silent server-side rejections of unknown names.
	Emotion string `json:"emotion,omitempty"`
	// Volume scales output gain in [0.5, 2.0]; 1.0 is neutral. Sonic-3
	// honors volume; sonic-3.5 has it temporarily disabled. We bump
	// volume for [shouts] to reinforce the anger envelope.
	Volume float64 `json:"volume,omitempty"`
}

type cartesiaOutputFormat struct {
	Container  string `json:"container"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

// Synthesize converts text to audio using Cartesia's REST API.
// For streaming output, use SynthesizeStream instead.
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
	// translated into the top-level `generation_config.emotion` field
	// (sonic-3+ API; the legacy voice.__experimental_controls.emotion is
	// silently ignored on current models) and stripped from the
	// transcript so they are not spoken literally. Plain text (no tags)
	// produces a request that omits generation_config, keeping the audio
	// cache key stable.
	transcript, genConfig := lowerCartesiaMarkup(text)
	if genConfig != nil {
		preview := transcript
		if len(preview) > logPreviewLen {
			preview = preview[:logPreviewLen] + "..."
		}
		logger.Info("cartesia: generation_config attached",
			"model", model,
			"voice", voice,
			"emotion", genConfig.Emotion,
			"volume", genConfig.Volume,
			"transcript_preview", preview)
	}

	reqBody := cartesiaRequest{
		ModelID:          model,
		Transcript:       transcript,
		Voice:            cartesiaVoiceConfig{Mode: "id", ID: voice},
		OutputFormat:     s.mapFormat(config.Format),
		Language:         config.Language,
		GenerationConfig: genConfig,
	}

	headers := map[string]string{
		"X-API-Key":        s.APIKey,
		headerContentType:  mimeJSON,
		"Cartesia-Version": "2026-03-01",
	}
	return postJSONForAudio(
		ctx, s.Client, providerCartesia, s.BaseURL+cartesiaRESTURL, reqBody, headers,
		func(resp *http.Response) error { return s.handleError(resp, voice) },
	)
}

// SpokenText reports the text Cartesia will actually speak for the given input:
// emotion tags become generation config, so the spoken transcript is the text
// with tags removed. Implements tts.SpokenTextReporter.
func (s *CartesiaService) SpokenText(text string, _ SynthesisConfig) string {
	transcript, _ := lowerCartesiaMarkup(text)
	return transcript
}

// lowerCartesiaMarkup translates characterization tags in text into the
// Cartesia request shape: a stripped transcript and an optional
// *cartesiaGenerationConfig carrying the chosen emotion (single string)
// and a volume bump for shout-style tags. Returns the raw text and a
// nil config when no actionable tags are present, keeping the audio
// cache key stable for plain text.
//
// Sonic-3 takes one emotion per utterance — if several tags appear, the
// first actionable tag wins. Tags with no Cartesia analog (whispers /
// calm / pause) drop out cleanly.
func lowerCartesiaMarkup(text string) (transcript string, cfg *cartesiaGenerationConfig) {
	tags := markup.ParseTags(text)
	if len(tags) == 0 {
		return text, nil
	}
	for _, t := range tags {
		if t.IsClose() {
			continue
		}
		emotion := cartesiaEmotionForTag(t)
		if emotion == "" {
			continue
		}
		cfg = &cartesiaGenerationConfig{Emotion: emotion}
		// Shout-style tags push volume to the top of Cartesia's allowed
		// range so the rage envelope is unambiguously audible on top of
		// emotion=angry. Sub-2.0 values (we previously tried 1.6) read
		// as polite annoyance on professional voices like Confident
		// Man; 2.0 lands as actual shouting. Calibrated against the
		// TestCartesiaShouts_ProbeVariants sweep.
		if t.Name == tagShouts || t.Name == tagShout {
			cfg.Volume = 2.0
		}
		break
	}
	return markup.StripTags(text), cfg
}

// cartesiaEmotionForTag maps a canonical markup tag onto Cartesia's
// sonic-3 emotion vocabulary, or returns "" when there is no sensible
// analog (the tag is then dropped). Sonic-3 accepts a fixed core set
// plus ~60 broader names; we map only the conservative core so the
// request is robust to silent server-side rejection of unknown values.
func cartesiaEmotionForTag(t markup.Tag) string {
	switch t.Name {
	case emotionExcited, "laughs", "laugh":
		return emotionExcited
	case "smile", "smiles":
		return "content"
	case emotionSad, "sighs", "sigh":
		return emotionSad
	case tagShouts, tagShout:
		return "angry"
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
	// Newer API versions describe an error with these instead of Error.
	ErrorCode string `json:"error_code,omitempty"`
	Title     string `json:"title,omitempty"`
	Message   string `json:"message,omitempty"`
}

// cartesiaWSTypeError is the type of a websocket frame that reports a failure.
const cartesiaWSTypeError = "error"

// errorText returns the frame's error description, or "" when it reports none.
// The pinned API version sends it in "error"; newer versions send a title and
// message instead, and a frame of type "error" is a failure either way.
func (r *cartesiaWSResponse) errorText() string {
	if r.Error != "" {
		return r.Error
	}
	if r.Type != cartesiaWSTypeError {
		return ""
	}
	var parts []string
	for _, p := range []string{r.Title, r.Message} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "error frame from server"
	}
	return strings.Join(parts, ": ")
}

// namingVoice prefixes message with the voice it is about when cause says the
// voice was rejected, so the caller sees which voice to fix.
func namingVoice(message, voice string, cause error) string {
	if errors.Is(cause, ErrInvalidVoice) {
		return fmt.Sprintf("voice %q: %s", voice, message)
	}
	return message
}

// processWSResponse processes a single WebSocket response and returns the audio chunk.
// Returns nil chunk if the response doesn't contain audio data.
// Returns error if processing fails or response contains an error; a rejected
// voice's error names voice.
func (s *CartesiaService) processWSResponse(
	resp *cartesiaWSResponse, index int, voice string,
) (*audio.Chunk, error) {
	if text := resp.errorText(); text != "" {
		retryable, cause := cartesiaWSErrorCause(resp)
		return nil, NewSynthesisError(
			providerCartesia, resp.ErrorCode, namingVoice(text, voice, cause), cause, retryable,
		)
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
			Encoding:   cartesiaEncodingPCMS16LE,
			SampleRate: sampleRate44100,
		}
	default:
		// Default to PCM raw format (also handles formatPCM explicitly). Raw
		// PCM has no header to carry its rate, so the requested rate is sent;
		// mp3 and wav self-describe and keep their fixed rate.
		rate := sampleRate24000
		if format.SampleRate > 0 {
			rate = format.SampleRate
		}
		return cartesiaOutputFormat{
			Container:  "raw",
			Encoding:   cartesiaEncodingPCMS16LE,
			SampleRate: rate,
		}
	}
}

// cartesiaErrorResponse represents an error response from Cartesia.
type cartesiaErrorResponse struct {
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

// handleError processes an error response from Cartesia to a request for
// voice. A 404 is the voice being rejected, and its error names the voice.
func (s *CartesiaService) handleError(resp *http.Response, voice string) error {
	var errResp cartesiaErrorResponse
	if e := decodeErrorBody(providerCartesia, resp, &errResp); e != nil {
		return e
	}
	retryable, cause := classifyHTTPStatus(resp.StatusCode, ErrInvalidVoice, fmt.Errorf("bad request"))
	message := errResp.Message
	if message == "" {
		message = errResp.Error
	}
	code := errResp.ErrorCode
	if code == "" {
		code = errResp.Error
	}
	cause = quotaCause(cause, resp.StatusCode, errResp.Error, errResp.ErrorCode)
	return NewSynthesisError(providerCartesia, code, namingVoice(message, voice, cause), cause, retryable)
}

// cartesiaWSErrorCause maps a websocket error frame to a cause and whether it
// is retryable. Cartesia's frames carry status_code (HTTP-style); 402 is an
// exhausted quota, 429 is throttling and 404 is a voice it does not serve.
func cartesiaWSErrorCause(resp *cartesiaWSResponse) (retryable bool, cause error) {
	retryable, cause = classifyHTTPStatus(resp.StatusCode, ErrInvalidVoice, nil)
	return retryable, quotaCause(cause, resp.StatusCode, resp.Error, resp.ErrorCode)
}

// SupportedVoices returns a sample of available Cartesia voices.
func (s *CartesiaService) SupportedVoices() []Voice {
	return []Voice{
		makeVoice(cartesiaDefaultVoice, "Katie - Friendly Fixer", "en", "female",
			"Enunciating American female for conversational support"),
		makeVoice("47c38ca4-5f35-497b-b1a3-415245fb35e1", "Daniel - Modern Assistant", "en", "male",
			"Clear, crisp American male for digital assistants"),
		makeVoice("62ae83ad-4f6a-430b-af41-a9bede9286ca", "Gemma - Decisive Agent", "en", "female",
			"Confident British female for professional assistance"),
		makeVoice("4bc3cb8c-adb9-4bb8-b5d5-cbbef950b991", "George - Composed Consultant", "en", "male",
			"Steady British male for capable assistance"),
		makeVoice("db6b0ed5-d5d3-463d-ae85-518a07d3c2b4", "Skylar - Friendly Guide", "en", "female",
			"Approachable American female for customer care"),
	}
}

// SupportedFormats returns audio formats supported by Cartesia.
func (s *CartesiaService) SupportedFormats() []AudioFormat {
	return []AudioFormat{
		makeFormat(formatPCM, "audio/pcm", sampleRate24000, bitDepth16),
		makeFormat(formatMP3, mimeAudioMPEG, sampleRate44100, 0),
		makeFormat(formatWAV, "audio/wav", sampleRate44100, bitDepth16),
	}
}
