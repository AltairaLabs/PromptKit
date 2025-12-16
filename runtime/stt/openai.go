package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	openAIBaseURL          = "https://api.openai.com/v1"
	openAITranscribeEndpoint = "/audio/transcriptions"

	// ModelWhisper1 is the OpenAI Whisper model for transcription.
	ModelWhisper1 = "whisper-1"

	// Default timeout for STT requests.
	defaultOpenAITimeout = 60 * time.Second

	// HTTP status code threshold for server errors.
	openAIServerErrorThreshold = 500
)

// OpenAIService implements STT using OpenAI's Whisper API.
type OpenAIService struct {
	apiKey  string
	baseURL string
	client  *http.Client
	model   string
}

// OpenAIOption configures the OpenAI STT service.
type OpenAIOption func(*OpenAIService)

// WithOpenAIBaseURL sets a custom base URL (for testing or proxies).
func WithOpenAIBaseURL(url string) OpenAIOption {
	return func(s *OpenAIService) {
		s.baseURL = url
	}
}

// WithOpenAIClient sets a custom HTTP client.
func WithOpenAIClient(client *http.Client) OpenAIOption {
	return func(s *OpenAIService) {
		s.client = client
	}
}

// WithOpenAIModel sets the STT model to use.
func WithOpenAIModel(model string) OpenAIOption {
	return func(s *OpenAIService) {
		s.model = model
	}
}

// NewOpenAI creates an OpenAI STT service using Whisper.
func NewOpenAI(apiKey string, opts ...OpenAIOption) *OpenAIService {
	s := &OpenAIService{
		apiKey:  apiKey,
		baseURL: openAIBaseURL,
		client:  &http.Client{Timeout: defaultOpenAITimeout},
		model:   ModelWhisper1,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name returns the provider identifier.
func (s *OpenAIService) Name() string {
	return "openai-whisper"
}

// normalizedConfig holds config values with defaults applied.
type normalizedConfig struct {
	format     string
	sampleRate int
	channels   int
	bitDepth   int
	model      string
}

// Transcribe converts audio to text using OpenAI's Whisper API.
//
//nolint:gocritic // hugeParam: TranscriptionConfig passed by value to satisfy Service interface
func (s *OpenAIService) Transcribe(
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
		nc.model = s.model
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

// buildMultipartForm builds the multipart form for the transcription request.
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

// executeRequest sends the transcription request and parses the response.
func (s *OpenAIService) executeRequest(
	ctx context.Context,
	formData *bytes.Buffer,
	contentType string,
) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.baseURL+openAITranscribeEndpoint,
		formData,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := s.client.Do(req)
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

// handleError processes an error response from OpenAI.
func (s *OpenAIService) handleError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err != nil {
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
