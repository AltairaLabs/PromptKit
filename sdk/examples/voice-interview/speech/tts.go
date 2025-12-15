package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// TTSVoice represents available TTS voices
type TTSVoice string

const (
	VoiceAlloy   TTSVoice = "alloy"
	VoiceEcho    TTSVoice = "echo"
	VoiceFable   TTSVoice = "fable"
	VoiceOnyx    TTSVoice = "onyx"
	VoiceNova    TTSVoice = "nova"
	VoiceShimmer TTSVoice = "shimmer"
)

// TTSFormat represents audio output formats
type TTSFormat string

const (
	FormatMP3  TTSFormat = "mp3"
	FormatOpus TTSFormat = "opus"
	FormatAAC  TTSFormat = "aac"
	FormatFLAC TTSFormat = "flac"
	FormatWAV  TTSFormat = "wav"
	FormatPCM  TTSFormat = "pcm"
)

// TextToSpeech converts text to audio
type TextToSpeech interface {
	// Synthesize converts text to audio bytes
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

// OpenAITTS uses OpenAI's TTS API
type OpenAITTS struct {
	apiKey     string
	model      string
	voice      TTSVoice
	format     TTSFormat
	speed      float64
	httpClient *http.Client
}

// TTSConfig holds configuration for TTS
type TTSConfig struct {
	APIKey string
	Voice  TTSVoice
	Format TTSFormat
	Speed  float64 // 0.25 to 4.0, default 1.0
}

// DefaultTTSConfig returns sensible defaults
func DefaultTTSConfig() TTSConfig {
	return TTSConfig{
		Voice:  VoiceNova, // Natural sounding voice
		Format: FormatPCM, // PCM for direct playback
		Speed:  1.0,
	}
}

// NewOpenAITTS creates a new OpenAI TTS service
func NewOpenAITTS(config TTSConfig) *OpenAITTS {
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	voice := config.Voice
	if voice == "" {
		voice = VoiceNova
	}

	format := config.Format
	if format == "" {
		format = FormatPCM
	}

	speed := config.Speed
	if speed == 0 {
		speed = 1.0
	}

	return &OpenAITTS{
		apiKey:     apiKey,
		model:      "tts-1", // Use tts-1 for lower latency, tts-1-hd for higher quality
		voice:      voice,
		format:     format,
		speed:      speed,
		httpClient: &http.Client{},
	}
}

// Synthesize converts text to audio using OpenAI TTS API
func (t *OpenAITTS) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key not configured")
	}

	if text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	// Create request body
	reqBody := struct {
		Model          string  `json:"model"`
		Input          string  `json:"input"`
		Voice          string  `json:"voice"`
		ResponseFormat string  `json:"response_format"`
		Speed          float64 `json:"speed"`
	}{
		Model:          t.model,
		Input:          text,
		Voice:          string(t.voice),
		ResponseFormat: string(t.format),
		Speed:          t.speed,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/speech", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TTS failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Read audio data
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio response: %w", err)
	}

	return audioData, nil
}

// GetSampleRate returns the sample rate for the configured format
// OpenAI TTS outputs at 24kHz for most formats
func (t *OpenAITTS) GetSampleRate() int {
	return 24000
}
