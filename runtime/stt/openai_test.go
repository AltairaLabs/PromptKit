package stt_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/stt"
)

func TestNewOpenAI(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")
	if service == nil {
		t.Fatal("NewOpenAI returned nil")
	}
	if service.Name() != "openai-whisper" {
		t.Errorf("Name() = %q, want %q", service.Name(), "openai-whisper")
	}
}

func TestOpenAIService_SupportedFormats(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")
	formats := service.SupportedFormats()

	if len(formats) == 0 {
		t.Fatal("SupportedFormats returned empty slice")
	}

	// Check for expected formats
	expectedFormats := []string{"wav", "mp3", "pcm"}
	for _, expected := range expectedFormats {
		found := false
		for _, format := range formats {
			if format == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SupportedFormats missing expected format: %s", expected)
		}
	}
}

func TestOpenAIService_Transcribe_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}

		// Verify authorization header
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			t.Errorf("Missing or invalid Authorization header: %s", authHeader)
		}

		// Return mock response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"text": "Hello, this is a test transcription.",
		})
	}))
	defer server.Close()

	// Create service with mock URL
	service := stt.NewOpenAI("test-api-key", stt.WithOpenAIBaseURL(server.URL))

	// Test transcription
	ctx := context.Background()
	audio := generateTestAudio(16000, 1.0) // 1 second of audio

	text, err := service.Transcribe(ctx, audio, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: 16000,
		Channels:   1,
		Language:   "en",
	})

	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	expected := "Hello, this is a test transcription."
	if text != expected {
		t.Errorf("Transcribe() = %q, want %q", text, expected)
	}
}

func TestOpenAIService_Transcribe_EmptyAudio(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")

	ctx := context.Background()
	_, err := service.Transcribe(ctx, []byte{}, stt.TranscriptionConfig{})

	if err == nil {
		t.Fatal("Expected error for empty audio, got nil")
	}

	if err != stt.ErrEmptyAudio {
		t.Errorf("Expected ErrEmptyAudio, got: %v", err)
	}
}

func TestOpenAIService_Transcribe_APIError(t *testing.T) {
	// Create mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid audio format",
				"type":    "invalid_request_error",
				"code":    "invalid_format",
			},
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", stt.WithOpenAIBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 1.0)

	_, err := service.Transcribe(ctx, audio, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: 16000,
	})

	if err == nil {
		t.Fatal("Expected error for API error response, got nil")
	}

	// Verify it's a TranscriptionError
	var txErr *stt.TranscriptionError
	if !isTranscriptionError(err, &txErr) {
		t.Errorf("Expected TranscriptionError, got: %T", err)
	}
}

func TestOpenAIService_Transcribe_RateLimited(t *testing.T) {
	// Create mock server that returns rate limit error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
				"code":    "rate_limit",
			},
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", stt.WithOpenAIBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 1.0)

	_, err := service.Transcribe(ctx, audio, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: 16000,
	})

	if err == nil {
		t.Fatal("Expected error for rate limit response, got nil")
	}

	// Verify it's retryable
	var txErr *stt.TranscriptionError
	if isTranscriptionError(err, &txErr) && !txErr.Retryable {
		t.Error("Rate limit error should be retryable")
	}
}

func TestOpenAIService_Transcribe_WithCustomClient(t *testing.T) {
	// Create a custom client that tracks calls
	callCount := 0
	customClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			// Return a mock response
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"text": "Test"}`)),
			}
			resp.Header.Set("Content-Type", "application/json")
			return resp, nil
		}),
	}

	service := stt.NewOpenAI("test-api-key", stt.WithOpenAIClient(customClient))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	_, err := service.Transcribe(ctx, audio, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: 16000,
	})

	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 HTTP call, got %d", callCount)
	}
}

func TestTranscriptionConfig_Defaults(t *testing.T) {
	config := stt.DefaultTranscriptionConfig()

	if config.Format != stt.FormatPCM {
		t.Errorf("Default Format = %q, want %q", config.Format, stt.FormatPCM)
	}
	if config.SampleRate != 16000 {
		t.Errorf("Default SampleRate = %d, want 16000", config.SampleRate)
	}
	if config.Channels != 1 {
		t.Errorf("Default Channels = %d, want 1", config.Channels)
	}
	if config.Language != "en" {
		t.Errorf("Default Language = %q, want %q", config.Language, "en")
	}
}

func TestWrapPCMAsWAV(t *testing.T) {
	// Generate some test PCM data
	pcmData := generateTestAudio(16000, 0.1) // 100ms

	// Wrap as WAV
	wavData := stt.WrapPCMAsWAV(pcmData, 16000, 1, 16)

	// WAV header should be 44 bytes
	if len(wavData) != len(pcmData)+44 {
		t.Errorf("WAV size = %d, want %d", len(wavData), len(pcmData)+44)
	}

	// Check RIFF header
	if string(wavData[0:4]) != "RIFF" {
		t.Errorf("Missing RIFF header, got: %s", string(wavData[0:4]))
	}

	// Check WAVE format
	if string(wavData[8:12]) != "WAVE" {
		t.Errorf("Missing WAVE format, got: %s", string(wavData[8:12]))
	}

	// Check fmt chunk
	if string(wavData[12:16]) != "fmt " {
		t.Errorf("Missing fmt chunk, got: %s", string(wavData[12:16]))
	}

	// Check data chunk
	if string(wavData[36:40]) != "data" {
		t.Errorf("Missing data chunk, got: %s", string(wavData[36:40]))
	}
}

// Helper functions

// generateTestAudio generates test PCM audio data (16-bit signed, little-endian)
func generateTestAudio(sampleRate int, durationSec float64) []byte {
	numSamples := int(float64(sampleRate) * durationSec)
	data := make([]byte, numSamples*2) // 16-bit = 2 bytes per sample

	// Generate simple sine wave
	for i := 0; i < numSamples; i++ {
		// Simple pattern - not a real audio signal but sufficient for testing
		sample := int16(i % 1000)
		data[i*2] = byte(sample & 0xFF)
		data[i*2+1] = byte((sample >> 8) & 0xFF)
	}

	return data
}

// roundTripFunc is a helper to create custom HTTP transport
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// isTranscriptionError checks if err is a TranscriptionError and sets the pointer
func isTranscriptionError(err error, target **stt.TranscriptionError) bool {
	if txErr, ok := err.(*stt.TranscriptionError); ok {
		*target = txErr
		return true
	}
	return false
}
