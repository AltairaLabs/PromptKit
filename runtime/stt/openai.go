package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// Compile-time check: OpenAIService must satisfy base.STTProvider and Service.
var (
	_ base.STTProvider = (*OpenAIService)(nil)
	_ Service          = (*OpenAIService)(nil)
)

const (
	openAIBaseURL            = "https://api.openai.com/v1"
	openAITranscribeEndpoint = "/audio/transcriptions"

	// ModelWhisper1 is the OpenAI Whisper model for transcription.
	ModelWhisper1 = "whisper-1"

	// Default timeout for STT requests.
	defaultOpenAITimeout = 60 * time.Second

	// HTTP status code threshold for server errors.
	openAIServerErrorThreshold = 500

	// openAIWhisperPerSecondRate is the per-second cost for OpenAI Whisper:
	// $0.006/minute = $0.0001/second.
	openAIWhisperPerSecondRate = 0.0001

	// bitsPerByte is the number of bits in a byte, used for PCM byte-rate computation.
	bitsPerByte = 8
	// unitSecond is the pricing/quantity unit for audio duration.
	unitSecond = "second"

	// responseFormatJSON is the response format every transcription model
	// accepts, and whose usage reports what the call billed: seconds on
	// whisper, tokens on the gpt-4o-*-transcribe models.
	responseFormatJSON = "json"
	// usageTypeDuration and usageTypeTokens are the json response's two
	// usage shapes.
	usageTypeDuration = "duration"
	usageTypeTokens   = "tokens"
)

// openAIDefaultPricing is the inline pricing descriptor for OpenAI Whisper.
// Rate: $0.006/minute = $0.0001/second (OpenAI public pricing).
var openAIDefaultPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: currencyUSD,
	Items: []base.PriceItem{
		{Unit: unitSecond, Rate: openAIWhisperPerSecondRate},
	},
}

// OpenAIService implements STT using OpenAI's Whisper API.
type OpenAIService struct {
	*base.Implementation    // provides Name, Type, Pricing, Validate, Init, HealthCheck, Close
	*base.HTTPServiceFields // APIKey, BaseURL, Model, Client
}

// OpenAIOption configures the OpenAI STT service.
// It is a type alias for base.HTTPServiceOption so callers can pass
// base.WithBaseURL, base.WithClient, base.WithModel, etc. directly.
type OpenAIOption = base.HTTPServiceOption

// NewOpenAI creates an OpenAI STT service using Whisper.
func NewOpenAI(apiKey string, opts ...OpenAIOption) *OpenAIService {
	impl, fields := base.NewHTTPService(apiKey, base.HTTPServiceDefaults{
		Name:    "openai-whisper",
		Type:    base.ProviderTypeSTT,
		Pricing: openAIDefaultPricing,
		BaseURL: openAIBaseURL,
		Model:   ModelWhisper1,
		Timeout: defaultOpenAITimeout,
	}, opts...)
	return &OpenAIService{Implementation: impl, HTTPServiceFields: fields}
}

// --- base.STTProvider ---

// Transcribe implements base.STTProvider. It converts audio to text and
// returns the transcript with cost metadata. Every model is asked for json,
// the only format the gpt-4o-*-transcribe models accept. Cost quantities come
// from the response's usage — seconds for duration-billed models (whisper),
// tokens for token-billed ones — or, when it has none, the audio length of
// PCM/WAV input.
func (s *OpenAIService) Transcribe(ctx context.Context, req base.STTRequest) (base.STTResponse, error) {
	if len(req.Audio) == 0 {
		return base.STTResponse{}, ErrEmptyAudio
	}

	start := time.Now()

	// Build a TranscriptionConfig from the STTRequest hints.
	cfg := transcriptionConfigFromRequest(req)
	nc := s.normalizeConfig(&cfg)

	audioData, filename := s.prepareAudio(req.Audio, nc)

	formData, contentType, err := s.buildMultipartForm(audioData, filename, nc, &cfg, responseFormatJSON)
	if err != nil {
		return base.STTResponse{}, err
	}

	text, quantities, err := s.executeRequestWithUsage(ctx, formData, contentType)
	if err != nil {
		return base.STTResponse{}, err
	}

	// No usage in the response: estimate seconds from the audio bytes.
	if len(quantities) == 0 {
		if secs := estimateAudioSeconds(req.Audio, req.MIMEType, nc); secs > 0 {
			quantities = map[string]float64{unitSecond: secs}
		}
	}

	latency := time.Since(start)
	resp := base.STTResponse{
		Text:    text,
		Latency: latency,
	}

	if pricing := s.Pricing(); pricing != nil && len(quantities) > 0 {
		resp.Cost = base.MakeCostInfo(
			pricing,
			s.Name(),
			base.ProviderTypeSTT,
			quantities,
			latency,
		)
	}

	return resp, nil
}

// transcriptionConfigFromRequest converts a base.STTRequest to a TranscriptionConfig
// using the Hints map for typed fields.
func transcriptionConfigFromRequest(req base.STTRequest) TranscriptionConfig {
	cfg := TranscriptionConfig{}
	if req.MIMEType != "" {
		cfg.Format = mimeToFormat(req.MIMEType)
	}
	if v := req.Hints["sample_rate"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.SampleRate = n
		}
	}
	if v := req.Hints["channels"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Channels = n
		}
	}
	if v := req.Hints["language"]; v != "" {
		cfg.Language = v
	}
	if v := req.Hints["model"]; v != "" {
		cfg.Model = v
	}
	return cfg
}

// mimeToFormat maps a MIME type to a format string.
func mimeToFormat(mime string) string {
	switch mime {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return FormatWAV
	case "audio/mpeg", "audio/mp3":
		return FormatMP3
	case "audio/pcm", "audio/l16":
		return FormatPCM
	default:
		return FormatPCM
	}
}

// estimateAudioSeconds computes approximate duration from byte count for PCM/WAV.
// Returns 0 when the format is not PCM/WAV (caller should rely on API response).
func estimateAudioSeconds(audio []byte, mimeType string, nc *normalizedConfig) float64 {
	switch nc.format {
	case FormatPCM, FormatWAV:
		if nc.sampleRate == 0 || nc.channels == 0 || nc.bitDepth == 0 {
			return 0
		}
		bytesPerSec := float64(nc.sampleRate * nc.channels * nc.bitDepth / bitsPerByte)
		if bytesPerSec <= 0 {
			return 0
		}
		return float64(len(audio)) / bytesPerSec
	default:
		_ = mimeType
		return 0
	}
}

// --- Service interface (legacy/TranscribeBytes) ---

// TranscribeBytes converts raw audio bytes to text using the legacy config shape.
// It returns the transcript only, with no cost.
func (s *OpenAIService) TranscribeBytes(
	ctx context.Context, audio []byte, config TranscriptionConfig,
) (string, error) {
	if len(audio) == 0 {
		return "", ErrEmptyAudio
	}

	nc := s.normalizeConfig(&config)
	audioData, filename := s.prepareAudio(audio, nc)

	formData, contentType, err := s.buildMultipartForm(audioData, filename, nc, &config, "")
	if err != nil {
		return "", err
	}

	return s.executeRequest(ctx, formData, contentType)
}

// --- internal helpers ---

// normalizedConfig holds config values with defaults applied.
type normalizedConfig struct {
	format     string
	sampleRate int
	channels   int
	bitDepth   int
	model      string
}

// normalizeConfig applies defaults to transcription config.
func (s *OpenAIService) normalizeConfig(config *TranscriptionConfig) *normalizedConfig {
	return normalizeTranscriptionConfig(config, s.Model)
}

// normalizeTranscriptionConfig applies the package defaults to config, and
// defaultModel when config names no model.
func normalizeTranscriptionConfig(config *TranscriptionConfig, defaultModel string) *normalizedConfig {
	nc := &normalizedConfig{
		format:     config.Format,
		sampleRate: config.SampleRate,
		channels:   config.Channels,
		bitDepth:   config.BitDepth,
		model:      config.Model,
	}
	if nc.format == "" {
		nc.format = FormatPCM
	}
	if nc.sampleRate == 0 {
		nc.sampleRate = DefaultSampleRate
	}
	if nc.channels == 0 {
		nc.channels = DefaultChannels
	}
	if nc.bitDepth == 0 {
		nc.bitDepth = DefaultBitDepth
	}
	if nc.model == "" {
		nc.model = defaultModel
	}
	return nc
}

// prepareAudio prepares audio data, wrapping PCM as WAV if needed.
func (s *OpenAIService) prepareAudio(
	audio []byte,
	nc *normalizedConfig,
) (audioData []byte, filename string) {
	return containerAudio(audio, nc)
}

// containerAudio returns audio as an uploadable file and its name: PCM
// wrapped as WAV, any other format as it is.
func containerAudio(audio []byte, nc *normalizedConfig) (audioData []byte, filename string) {
	if nc.format == FormatPCM {
		return WrapPCMAsWAV(audio, nc.sampleRate, nc.channels, nc.bitDepth), "audio.wav"
	}
	return audio, "audio." + nc.format
}

// buildMultipartForm builds the multipart form for the transcription request.
// A non-empty responseFormat is sent as response_format; empty leaves the
// API default (json).
func (s *OpenAIService) buildMultipartForm(
	audioData []byte,
	filename string,
	nc *normalizedConfig,
	config *TranscriptionConfig,
	responseFormat string,
) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := s.writeAudioField(writer, audioData, filename); err != nil {
		return nil, "", err
	}

	if err := s.writeModelField(writer, nc.model); err != nil {
		return nil, "", err
	}

	if err := s.writeOptionalFields(writer, config); err != nil {
		return nil, "", err
	}

	if responseFormat != "" {
		if err := writer.WriteField("response_format", responseFormat); err != nil {
			return nil, "", fmt.Errorf("failed to write response_format field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

// writeAudioField writes the audio file field.
func (s *OpenAIService) writeAudioField(writer *multipart.Writer, audioData []byte, filename string) error {
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return fmt.Errorf("failed to write audio data: %w", err)
	}
	return nil
}

// writeModelField writes the model field.
func (s *OpenAIService) writeModelField(writer *multipart.Writer, model string) error {
	if err := writer.WriteField("model", model); err != nil {
		return fmt.Errorf("failed to write model field: %w", err)
	}
	return nil
}

// writeOptionalFields writes optional language and prompt fields.
func (s *OpenAIService) writeOptionalFields(writer *multipart.Writer, config *TranscriptionConfig) error {
	if config.Language != "" {
		if err := writer.WriteField("language", config.Language); err != nil {
			return fmt.Errorf("failed to write language field: %w", err)
		}
	}
	if config.Prompt != "" {
		if err := writer.WriteField("prompt", config.Prompt); err != nil {
			return fmt.Errorf("failed to write prompt field: %w", err)
		}
	}
	return nil
}

// doTranscriptionRequest sends the multipart-form transcription request and
// returns the raw response body. Non-200 responses are converted to errors via
// handleError.
func (s *OpenAIService) doTranscriptionRequest(
	ctx context.Context,
	formData *bytes.Buffer,
	contentType string,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, s.BaseURL+openAITranscribeEndpoint, formData,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if s.APIKey != "" {
		// Empty when a platform credential authenticates through the client.
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, NewTranscriptionError("openai", "", "request failed", err, true)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, withRetryAfter(s.handleError(resp.StatusCode, body), resp)
	}
	return body, nil
}

// executeRequest sends the transcription request and parses the simple response.
func (s *OpenAIService) executeRequest(
	ctx context.Context, formData *bytes.Buffer, contentType string,
) (string, error) {
	body, err := s.doTranscriptionRequest(ctx, formData, contentType)
	if err != nil {
		return "", err
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	return result.Text, nil
}

// jsonResponse is the json transcription body. Usage is optional.
type jsonResponse struct {
	Text  string              `json:"text"`
	Usage *transcriptionUsage `json:"usage"`
}

// transcriptionUsage is the response's usage, in one of two shapes told
// apart by Type.
type transcriptionUsage struct {
	Type string `json:"type"` // "duration" or "tokens"
	// Duration-billed models.
	Seconds float64 `json:"seconds"`
	// Token-billed models.
	InputTokens       float64 `json:"input_tokens"`
	OutputTokens      float64 `json:"output_tokens"`
	InputTokenDetails *struct {
		AudioTokens float64 `json:"audio_tokens"`
		TextTokens  float64 `json:"text_tokens"`
	} `json:"input_token_details"`
}

// quantities returns the billed quantities the response's usage reports,
// keyed by cost unit, or nil when it reports none.
func (r *jsonResponse) quantities() map[string]float64 {
	q := map[string]float64{}
	if u := r.Usage; u != nil {
		u.addTo(q)
	}
	if len(q) == 0 {
		return nil
	}
	return q
}

// addTo adds the usage's billed quantities to q.
func (u *transcriptionUsage) addTo(q map[string]float64) {
	switch u.Type {
	case usageTypeDuration:
		addQuantity(q, unitSecond, u.Seconds)
	case usageTypeTokens:
		if d := u.InputTokenDetails; d != nil {
			addQuantity(q, base.UnitAudioInputToken, d.AudioTokens)
			addQuantity(q, base.UnitInputToken, d.TextTokens)
		} else {
			// The audio file is the input; text input is only a prompt,
			// which the details would break out.
			addQuantity(q, base.UnitAudioInputToken, u.InputTokens)
		}
		addQuantity(q, base.UnitOutputToken, u.OutputTokens)
	}
}

func addQuantity(q map[string]float64, unit string, n float64) {
	if n > 0 {
		q[unit] = n
	}
}

// executeRequestWithUsage sends the transcription request and parses a json
// response, returning the transcript and the quantities its usage bills.
func (s *OpenAIService) executeRequestWithUsage(
	ctx context.Context, formData *bytes.Buffer, contentType string,
) (text string, quantities map[string]float64, err error) {
	body, err := s.doTranscriptionRequest(ctx, formData, contentType)
	if err != nil {
		return "", nil, err
	}
	var result jsonResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return result.Text, result.quantities(), nil
}

// handleError processes an error response from OpenAI.
func (s *OpenAIService) handleError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	if json.Unmarshal(body, &errResp) != nil {
		return NewTranscriptionError(
			"openai",
			fmt.Sprintf("%d", statusCode),
			string(body),
			nil,
			statusCode >= openAIServerErrorThreshold,
		)
	}

	retryable := statusCode == http.StatusTooManyRequests ||
		statusCode >= openAIServerErrorThreshold

	var cause error
	switch statusCode {
	case http.StatusTooManyRequests:
		cause = ErrRateLimited
	case http.StatusUnauthorized:
		cause = fmt.Errorf("invalid API key")
	case http.StatusBadRequest:
		if errResp.Error.Code == "audio_too_short" {
			cause = ErrAudioTooShort
		}
	}

	return NewTranscriptionError(
		"openai",
		errResp.Error.Code,
		errResp.Error.Message,
		cause,
		retryable,
	)
}

// SupportedFormats returns audio formats supported by OpenAI Whisper.
func (s *OpenAIService) SupportedFormats() []string {
	return []string{
		formatFLAC,
		formatM4A,
		"mp3",
		"mp4",
		"mpeg",
		"mpga",
		"oga",
		formatOGG,
		"wav",
		formatWebM,
		"pcm", // Wrapped as WAV internally
	}
}
