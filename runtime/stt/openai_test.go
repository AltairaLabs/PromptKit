package stt_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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

func TestOpenAIService_Type(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")
	if service.Type() != base.ProviderTypeSTT {
		t.Errorf("Type() = %q, want %q", service.Type(), base.ProviderTypeSTT)
	}
}

func TestOpenAIService_Pricing_Default(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")
	p := service.Pricing()
	if p == nil {
		t.Fatal("Pricing() returned nil for default pricing")
	}
	if len(p.Items) == 0 {
		t.Fatal("Pricing().Items is empty")
	}
	if p.Items[0].Unit != "second" {
		t.Errorf("pricing unit = %q, want %q", p.Items[0].Unit, "second")
	}
	if p.Items[0].Rate <= 0 {
		t.Error("pricing rate must be > 0")
	}
}

func TestOpenAIService_Pricing_Override(t *testing.T) {
	custom := &base.PricingDescriptor{
		Source:   base.PricingSourceInline,
		Currency: "usd",
		Items:    []base.PriceItem{{Unit: "second", Rate: 0.0002}},
	}
	service := stt.NewOpenAI("test-api-key")
	service.SetPricing(custom)
	if service.Pricing() != custom {
		t.Error("SetPricing did not override pricing")
	}
}

func TestOpenAIService_BaseProviderMethods(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")
	if err := service.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	if err := service.Init(context.Background()); err != nil {
		t.Errorf("Init() = %v, want nil", err)
	}
	if err := service.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() = %v, want nil", err)
	}
	if err := service.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
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

// TestOpenAIService_Transcribe_Request tests the base.STTProvider Transcribe method
// with a mock server returning verbose_json with a duration field.
func TestOpenAIService_Transcribe_Request_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			t.Errorf("Missing or invalid Authorization header: %s", authHeader)
		}
		// Return verbose_json response with duration
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":     "Hello, this is a test transcription.",
			"duration": 1.5,
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 1.0) // 1 second of audio

	resp, err := service.Transcribe(ctx, base.STTRequest{
		Audio:    audio,
		MIMEType: "audio/pcm",
		Hints:    map[string]string{"sample_rate": "16000", "language": "en"},
	})

	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	expected := "Hello, this is a test transcription."
	if resp.Text != expected {
		t.Errorf("resp.Text = %q, want %q", resp.Text, expected)
	}

	// Cost must be computed from the duration returned by the API (1.5s).
	if resp.Cost == nil {
		t.Fatal("expected Cost to be populated")
	}
	if resp.Cost.TotalCost <= 0 {
		t.Errorf("TotalCost = %v, want > 0", resp.Cost.TotalCost)
	}
	// 1.5 seconds * 0.0001 USD/s = 0.00015
	wantCost := 1.5 * 0.0001
	if diff := resp.Cost.TotalCost - wantCost; diff < -0.000001 || diff > 0.000001 {
		t.Errorf("TotalCost = %v, want ~%v", resp.Cost.TotalCost, wantCost)
	}
	if resp.Cost.Quantities["second"] != 1.5 {
		t.Errorf("Quantities[second] = %v, want 1.5", resp.Cost.Quantities["second"])
	}
}

// TestOpenAIService_Transcribe_Request_FallbackEstimate tests cost estimation
// from audio byte length when the API response omits the duration field.
func TestOpenAIService_Transcribe_Request_FallbackEstimate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return verbose_json response WITHOUT duration (simulates older API or edge case)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text": "Fallback estimate test.",
			// duration intentionally absent — defaults to 0
		})
	}))
	defer server.Close()

	// 16000 Hz, 16-bit mono, 1 second = 32000 bytes
	audio := generateTestAudio(16000, 1.0)
	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	resp, err := service.Transcribe(context.Background(), base.STTRequest{
		Audio:    audio,
		MIMEType: "audio/pcm",
		Hints:    map[string]string{"sample_rate": "16000"},
	})
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}
	if resp.Cost == nil {
		t.Fatal("expected Cost to be populated via byte-length estimate")
	}
	// 32000 bytes / (16000 * 1 * 2 bytes/sample) = 1.0 second
	wantCost := 1.0 * 0.0001
	if diff := resp.Cost.TotalCost - wantCost; diff < -0.000001 || diff > 0.000001 {
		t.Errorf("TotalCost = %v, want ~%v", resp.Cost.TotalCost, wantCost)
	}
}

func TestOpenAIService_Transcribe_Request_EmptyAudio(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")
	_, err := service.Transcribe(context.Background(), base.STTRequest{})
	if err == nil {
		t.Fatal("Expected error for empty audio, got nil")
	}
	if err != stt.ErrEmptyAudio {
		t.Errorf("Expected ErrEmptyAudio, got: %v", err)
	}
}

// TestOpenAIService_Transcribe_Request_NoPricing verifies that no-pricing returns nil Cost.
func TestOpenAIService_Transcribe_Request_NoPricing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":     "No pricing test.",
			"duration": 1.0,
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))
	service.SetPricing(nil)

	resp, err := service.Transcribe(context.Background(), base.STTRequest{
		Audio:    generateTestAudio(16000, 1.0),
		MIMEType: "audio/pcm",
	})
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}
	if resp.Cost != nil {
		t.Errorf("expected nil Cost for nil-pricing provider, got %+v", resp.Cost)
	}
}

// --- Legacy TranscribeBytes tests ---

func TestOpenAIService_TranscribeBytes_Success(t *testing.T) {
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
	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	// Test transcription
	ctx := context.Background()
	audio := generateTestAudio(16000, 1.0) // 1 second of audio

	text, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: 16000,
		Channels:   1,
		Language:   "en",
	})

	if err != nil {
		t.Fatalf("TranscribeBytes failed: %v", err)
	}

	expected := "Hello, this is a test transcription."
	if text != expected {
		t.Errorf("TranscribeBytes() = %q, want %q", text, expected)
	}
}

func TestOpenAIService_TranscribeBytes_EmptyAudio(t *testing.T) {
	service := stt.NewOpenAI("test-api-key")

	ctx := context.Background()
	_, err := service.TranscribeBytes(ctx, []byte{}, stt.TranscriptionConfig{})

	if err == nil {
		t.Fatal("Expected error for empty audio, got nil")
	}

	if err != stt.ErrEmptyAudio {
		t.Errorf("Expected ErrEmptyAudio, got: %v", err)
	}
}

func TestOpenAIService_TranscribeBytes_APIError(t *testing.T) {
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

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 1.0)

	_, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
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

func TestOpenAIService_TranscribeBytes_RateLimited(t *testing.T) {
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

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 1.0)

	_, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
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

func TestOpenAIService_TranscribeBytes_WithCustomClient(t *testing.T) {
	// Create a custom client that tracks calls
	callCount := 0
	customClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			// Return a mock response
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"text": "Test"}`)),
			}
			resp.Header.Set("Content-Type", "application/json")
			return resp, nil
		}),
	}

	service := stt.NewOpenAI("test-api-key", base.WithClient(customClient))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	_, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: 16000,
	})

	if err != nil {
		t.Fatalf("TranscribeBytes failed: %v", err)
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

// =============================================================================
// Additional Helper Function Tests (for coverage)
// =============================================================================

func TestOpenAIService_TranscribeBytes_WithPrompt(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify multipart form
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("Failed to parse multipart form: %v", err)
		}

		// Check prompt field was included
		prompt := r.FormValue("prompt")
		if prompt != "test context prompt" {
			t.Errorf("Expected prompt field, got: %q", prompt)
		}

		// Return mock response
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"text": "Transcription with context",
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	text, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: 16000,
		Channels:   1,
		Prompt:     "test context prompt",
	})

	if err != nil {
		t.Fatalf("TranscribeBytes failed: %v", err)
	}

	if text != "Transcription with context" {
		t.Errorf("Unexpected text: %q", text)
	}
}

func TestOpenAIService_TranscribeBytes_WAVFormat(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"text": "WAV transcription",
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	// Use WAV format (no wrapping needed)
	audio := generateTestAudio(16000, 0.5)

	text, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format:     "wav",
		SampleRate: 16000,
		Channels:   1,
	})

	if err != nil {
		t.Fatalf("TranscribeBytes failed: %v", err)
	}

	if text != "WAV transcription" {
		t.Errorf("Unexpected text: %q", text)
	}
}

func TestOpenAIService_TranscribeBytes_DefaultsApplied(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"text": "Default config test",
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	// Use empty config - defaults should be applied
	text, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{})

	if err != nil {
		t.Fatalf("TranscribeBytes failed: %v", err)
	}

	if text != "Default config test" {
		t.Errorf("Unexpected text: %q", text)
	}
}

func TestOpenAIService_TranscribeBytes_CustomModel(t *testing.T) {
	// Create mock server
	modelReceived := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("Failed to parse multipart form: %v", err)
		}
		modelReceived = r.FormValue("model")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"text": "Custom model test",
		})
	}))
	defer server.Close()

	// Create service with custom model
	service := stt.NewOpenAI("test-api-key",
		base.WithBaseURL(server.URL),
		base.WithModel("custom-whisper-model"))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	_, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format: stt.FormatPCM,
	})

	if err != nil {
		t.Fatalf("TranscribeBytes failed: %v", err)
	}

	if modelReceived != "custom-whisper-model" {
		t.Errorf("Expected custom model, got: %q", modelReceived)
	}
}

func TestOpenAIService_TranscribeBytes_UnauthorizedError(t *testing.T) {
	// Create mock server that returns 401
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid API key",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("invalid-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	_, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format: stt.FormatPCM,
	})

	if err == nil {
		t.Fatal("Expected error for unauthorized request")
	}

	// Should be a TranscriptionError
	var txErr *stt.TranscriptionError
	if !isTranscriptionError(err, &txErr) {
		t.Errorf("Expected TranscriptionError, got: %T", err)
	}
}

func TestOpenAIService_TranscribeBytes_ServerError(t *testing.T) {
	// Create mock server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Internal server error",
				"type":    "server_error",
				"code":    "server_error",
			},
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	_, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format: stt.FormatPCM,
	})

	if err == nil {
		t.Fatal("Expected error for server error response")
	}

	// Server errors should be retryable
	var txErr *stt.TranscriptionError
	if isTranscriptionError(err, &txErr) && !txErr.Retryable {
		t.Error("Server error should be retryable")
	}
}

func TestOpenAIService_TranscribeBytes_MalformedResponse(t *testing.T) {
	// Create mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json{"))
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-key", base.WithBaseURL(server.URL))

	ctx := context.Background()
	audio := generateTestAudio(16000, 0.5)

	_, err := service.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{
		Format: stt.FormatPCM,
	})

	if err == nil {
		t.Fatal("Expected error for malformed response")
	}
}

// TestOpenAIService_Transcribe_MP3MIMEType verifies that an MP3 MIME type is accepted
// and the fallback estimate returns 0 (non-PCM/WAV formats rely on the API duration).
func TestOpenAIService_Transcribe_MP3MIMEType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":     "MP3 test.",
			"duration": 2.0,
		})
	}))
	defer server.Close()

	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))
	resp, err := service.Transcribe(context.Background(), base.STTRequest{
		Audio:    generateTestAudio(16000, 1.0),
		MIMEType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("Transcribe MP3 failed: %v", err)
	}
	if resp.Text != "MP3 test." {
		t.Errorf("Text = %q, want %q", resp.Text, "MP3 test.")
	}
	// For non-PCM MIME, cost comes from API-reported duration (2.0s).
	if resp.Cost == nil {
		t.Fatal("expected Cost from API duration")
	}
	wantCost := 2.0 * 0.0001
	if diff := resp.Cost.TotalCost - wantCost; diff < -0.000001 || diff > 0.000001 {
		t.Errorf("TotalCost = %v, want ~%v", resp.Cost.TotalCost, wantCost)
	}
}

// TestOpenAIService_Transcribe_WAVMIMEType verifies WAV MIME type routing and
// that the byte-length fallback fires when the API omits duration.
func TestOpenAIService_Transcribe_WAVMIMEType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No "duration" field — fallback to byte-length estimate.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text": "WAV mime test.",
		})
	}))
	defer server.Close()

	// 16000 Hz, 16-bit mono, 0.5s → 16000 bytes
	audio := generateTestAudio(16000, 0.5)
	service := stt.NewOpenAI("test-api-key", base.WithBaseURL(server.URL))
	resp, err := service.Transcribe(context.Background(), base.STTRequest{
		Audio:    audio,
		MIMEType: "audio/wav",
		Hints:    map[string]string{"sample_rate": "16000"},
	})
	if err != nil {
		t.Fatalf("Transcribe WAV failed: %v", err)
	}
	if resp.Cost == nil {
		t.Fatal("expected Cost from byte-length estimate for WAV")
	}
	// 16000 bytes / (16000 * 1 * 16 / 8) = 0.5s
	wantCost := 0.5 * 0.0001
	if diff := resp.Cost.TotalCost - wantCost; diff < -0.000001 || diff > 0.000001 {
		t.Errorf("TotalCost = %v, want ~%v", resp.Cost.TotalCost, wantCost)
	}
}
