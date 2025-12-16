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

// Transcribe converts audio to text using OpenAI's Whisper API.
//
//nolint:gocritic // hugeParam: TranscriptionConfig passed by value to satisfy Service interface
func (s *OpenAIService) Transcribe(
	ctx context.Context, audio []byte, config TranscriptionConfig,
) (string, error) {
	if len(audio) == 0 {
		return "", ErrEmptyAudio
	}

	// Apply defaults
	format := config.Format
	if format == "" {
		format = FormatPCM
	}
	sampleRate := config.SampleRate
	if sampleRate == 0 {
		sampleRate = DefaultSampleRate
	}
	channels := config.Channels
	if channels == 0 {
		channels = DefaultChannels
	}
	bitDepth := config.BitDepth
	if bitDepth == 0 {
		bitDepth = DefaultBitDepth
	}

	// Prepare audio data
	audioData := audio
	filename := "audio." + format

	// PCM needs to be wrapped as WAV for Whisper
	if format == FormatPCM {
		audioData = WrapPCMAsWAV(audio, sampleRate, channels, bitDepth)
		filename = "audio.wav"
	}

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add audio file
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model field
	model := config.Model
	if model == "" {
		model = s.model
	}
	if err := writer.WriteField("model", model); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// Add language hint if provided
	if config.Language != "" {
		if err := writer.WriteField("language", config.Language); err != nil {
			return "", fmt.Errorf("failed to write language field: %w", err)
		}
	}

	// Add prompt if provided
	if config.Prompt != "" {
		if err := writer.WriteField("prompt", config.Prompt); err != nil {
			return "", fmt.Errorf("failed to write prompt field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.baseURL+openAITranscribeEndpoint,
		&buf,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
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

	// Parse response
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
