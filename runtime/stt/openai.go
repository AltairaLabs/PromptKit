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

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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
)

// openAIDefaultPricing is the inline pricing descriptor for OpenAI Whisper.
// Rate: $0.006/minute = $0.0001/second (OpenAI public pricing).
var openAIDefaultPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: "usd",
	Items: []base.PriceItem{
		{Unit: "second", Rate: openAIWhisperPerSecondRate},
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

// WithOpenAIPricing overrides the default pricing descriptor for this instance.
// Delegates to the embedded base.Implementation's SetPricing.
func WithOpenAIPricing(p *base.PricingDescriptor) func(*OpenAIService) {
	return func(s *OpenAIService) {
		s.SetPricing(p)
	}
}

// NewOpenAI creates an OpenAI STT service using Whisper.
func NewOpenAI(apiKey string, opts ...OpenAIOption) *OpenAIService {
	s := &OpenAIService{
		Implementation: base.NewImplementation("openai-whisper", base.ProviderTypeSTT, openAIDefaultPricing),
		HTTPServiceFields: &base.HTTPServiceFields{
			APIKey:  apiKey,
			BaseURL: openAIBaseURL,
			Client:  &http.Client{Timeout: defaultOpenAITimeout},
			Model:   ModelWhisper1,
		},
	}
	for _, opt := range opts {
		opt(s.HTTPServiceFields)
	}
	return s
}

// --- base.STTProvider ---

// Transcribe implements base.STTProvider. It converts audio to text, computes
// per-second cost from the pricing descriptor, and returns both the transcript
// and cost metadata. Duration is estimated from the audio length for PCM/WAV
// input, or from the API's verbose_json response for other formats.
func (s *OpenAIService) Transcribe(ctx context.Context, req base.STTRequest) (base.STTResponse, error) {
	if len(req.Audio) == 0 {
		return base.STTResponse{}, ErrEmptyAudio
	}

	start := time.Now()

	// Build a TranscriptionConfig from the STTRequest hints.
	cfg := transcriptionConfigFromRequest(req)
	nc := s.normalizeConfig(&cfg)

	audioData, filename := s.prepareAudio(req.Audio, nc)

	formData, contentType, err := s.buildMultipartFormVerbose(audioData, filename, nc, &cfg)
	if err != nil {
		return base.STTResponse{}, err
	}

	text, audioSeconds, err := s.executeRequestVerbose(ctx, formData, contentType)
	if err != nil {
		return base.STTResponse{}, err
	}

	// If the API did not return a duration (non-verbose fallback), estimate from audio bytes.
	if audioSeconds <= 0 {
		audioSeconds = estimateAudioSeconds(req.Audio, req.MIMEType, nc)
	}

	latency := time.Since(start)
	resp := base.STTResponse{
		Text:    text,
		Latency: latency,
	}

	if pricing := s.Pricing(); pricing != nil && audioSeconds > 0 {
		resp.Cost = base.MakeCostInfo(
			pricing,
			s.Name(),
			base.ProviderTypeSTT,
			map[string]float64{"second": audioSeconds},
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
// It delegates to the Whisper API using the simple (non-verbose) response format.
//
//nolint:gocritic // hugeParam: TranscriptionConfig passed by value to satisfy Service interface
func (s *OpenAIService) TranscribeBytes(
	ctx context.Context, audio []byte, config TranscriptionConfig,
) (string, error) {
	if len(audio) == 0 {
		return "", ErrEmptyAudio
	}

	nc := s.normalizeConfig(&config)
	audioData, filename := s.prepareAudio(audio, nc)

	formData, contentType, err := s.buildMultipartForm(audioData, filename, nc, &config)
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
		nc.model = s.Model
	}
	return nc
}

// prepareAudio prepares audio data, wrapping PCM as WAV if needed.
func (s *OpenAIService) prepareAudio(
	audio []byte,
	nc *normalizedConfig,
) (audioData []byte, filename string) {
	if nc.format == FormatPCM {
		return WrapPCMAsWAV(audio, nc.sampleRate, nc.channels, nc.bitDepth), "audio.wav"
	}
	return audio, "audio." + nc.format
}

// buildMultipartForm builds the multipart form for the transcription request
// using the simple (non-verbose) response format.
func (s *OpenAIService) buildMultipartForm(
	audioData []byte,
	filename string,
	nc *normalizedConfig,
	config *TranscriptionConfig,
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

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

// buildMultipartFormVerbose builds the multipart form requesting verbose_json
// so that the API returns a duration field for cost computation.
func (s *OpenAIService) buildMultipartFormVerbose(
	audioData []byte,
	filename string,
	nc *normalizedConfig,
	config *TranscriptionConfig,
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

	// Request verbose_json to get duration for cost computation.
	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return nil, "", fmt.Errorf("failed to write response_format field: %w", err)
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

// executeRequest sends the transcription request and parses the simple response.
func (s *OpenAIService) executeRequest(
	ctx context.Context,
	formData *bytes.Buffer,
	contentType string,
) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.BaseURL+openAITranscribeEndpoint,
		formData,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", NewTranscriptionError("openai", "", "request failed", err, true)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", s.handleError(resp.StatusCode, body)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Text, nil
}

// verboseResponse is the shape returned by OpenAI Whisper with response_format=verbose_json.
type verboseResponse struct {
	Text     string  `json:"text"`
	Duration float64 `json:"duration"` // audio duration in seconds
}

// executeRequestVerbose sends the transcription request and parses a verbose_json
// response, extracting both the transcript text and the audio duration.
func (s *OpenAIService) executeRequestVerbose(
	ctx context.Context,
	formData *bytes.Buffer,
	contentType string,
) (text string, audioSeconds float64, err error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.BaseURL+openAITranscribeEndpoint,
		formData,
	)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", 0, NewTranscriptionError("openai", "", "request failed", err, true)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, s.handleError(resp.StatusCode, body)
	}

	var result verboseResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Text, result.Duration, nil
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
		"flac",
		"m4a",
		"mp3",
		"mp4",
		"mpeg",
		"mpga",
		"oga",
		"ogg",
		"wav",
		"webm",
		"pcm", // Wrapped as WAV internally
	}
}
