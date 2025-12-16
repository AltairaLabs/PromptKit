package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewOpenAI(t *testing.T) {
	service := NewOpenAI("test-key")
	if service == nil {
		t.Fatal("NewOpenAI() returned nil")
	}

	if service.apiKey != "test-key" {
		t.Errorf("apiKey = %v, want test-key", service.apiKey)
	}

	if service.baseURL != openAIBaseURL {
		t.Errorf("baseURL = %v, want %v", service.baseURL, openAIBaseURL)
	}

	if service.model != ModelTTS1 {
		t.Errorf("model = %v, want %v", service.model, ModelTTS1)
	}
}

func TestNewOpenAI_WithOptions(t *testing.T) {
	customClient := &http.Client{}
	service := NewOpenAI("test-key",
		WithOpenAIBaseURL("https://custom.api.com"),
		WithOpenAIClient(customClient),
		WithOpenAIModel(ModelTTS1HD),
	)

	if service.baseURL != "https://custom.api.com" {
		t.Errorf("baseURL = %v, want https://custom.api.com", service.baseURL)
	}

	if service.client != customClient {
		t.Error("client was not set correctly")
	}

	if service.model != ModelTTS1HD {
		t.Errorf("model = %v, want %v", service.model, ModelTTS1HD)
	}
}

func TestOpenAIService_Name(t *testing.T) {
	service := NewOpenAI("test-key")
	if service.Name() != "openai" {
		t.Errorf("Name() = %v, want openai", service.Name())
	}
}

func TestOpenAIService_Synthesize_EmptyText(t *testing.T) {
	service := NewOpenAI("test-key")
	_, err := service.Synthesize(context.Background(), "", SynthesisConfig{})
	if err != ErrEmptyText {
		t.Errorf("Synthesize() error = %v, want ErrEmptyText", err)
	}
}

func TestOpenAIService_Synthesize_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("Method = %v, want POST", r.Method)
		}

		if !strings.HasSuffix(r.URL.Path, "/audio/speech") {
			t.Errorf("Path = %v, want /audio/speech", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization = %v, want Bearer test-key", auth)
		}

		// Verify body
		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Input != "Hello world" {
			t.Errorf("Input = %v, want Hello world", req.Input)
		}

		if req.Voice != "alloy" {
			t.Errorf("Voice = %v, want alloy", req.Voice)
		}

		// Return mock audio
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("mock audio data"))
	}))
	defer server.Close()

	service := NewOpenAI("test-key", WithOpenAIBaseURL(server.URL))

	reader, err := service.Synthesize(context.Background(), "Hello world", SynthesisConfig{
		Voice: "alloy",
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if string(data) != "mock audio data" {
		t.Errorf("data = %v, want mock audio data", string(data))
	}
}

func TestOpenAIService_Synthesize_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	service := NewOpenAI("test-key", WithOpenAIBaseURL(server.URL))

	_, err := service.Synthesize(context.Background(), "Hello", SynthesisConfig{})
	if err == nil {
		t.Fatal("Synthesize() should return error")
	}

	var synthErr *SynthesisError
	if !isError(err, &synthErr) {
		t.Fatalf("error should be SynthesisError, got %T", err)
	}

	if !synthErr.Retryable {
		t.Error("error should be retryable")
	}
}

func TestOpenAIService_SupportedVoices(t *testing.T) {
	service := NewOpenAI("test-key")
	voices := service.SupportedVoices()

	if len(voices) != 6 {
		t.Errorf("len(SupportedVoices()) = %v, want 6", len(voices))
	}

	// Verify all expected voices are present
	expectedVoices := []string{"alloy", "echo", "fable", "onyx", "nova", "shimmer"}
	for _, expected := range expectedVoices {
		found := false
		for _, v := range voices {
			if v.ID == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Voice %v not found in SupportedVoices()", expected)
		}
	}
}

func TestOpenAIService_SupportedFormats(t *testing.T) {
	service := NewOpenAI("test-key")
	formats := service.SupportedFormats()

	if len(formats) < 5 {
		t.Errorf("len(SupportedFormats()) = %v, want >= 5", len(formats))
	}
}

func TestOpenAIService_mapFormat(t *testing.T) {
	service := NewOpenAI("test-key")

	tests := []struct {
		format AudioFormat
		want   string
	}{
		{FormatMP3, "mp3"},
		{FormatOpus, "opus"},
		{FormatAAC, "aac"},
		{FormatFLAC, "flac"},
		{FormatWAV, "wav"},
		{FormatPCM16, "pcm"},
		{AudioFormat{Name: "unknown"}, "mp3"},
	}

	for _, tt := range tests {
		t.Run(tt.format.Name, func(t *testing.T) {
			if got := service.mapFormat(tt.format); got != tt.want {
				t.Errorf("mapFormat(%v) = %v, want %v", tt.format.Name, got, tt.want)
			}
		})
	}
}

func TestOpenAIService_Synthesize_WithConfig(t *testing.T) {
	var receivedReq openAIRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewOpenAI("test-key", WithOpenAIBaseURL(server.URL))

	config := SynthesisConfig{
		Voice:  "nova",
		Format: FormatOpus,
		Speed:  1.5,
		Model:  ModelTTS1HD,
	}

	reader, err := service.Synthesize(context.Background(), "Test", config)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	reader.Close()

	if receivedReq.Voice != "nova" {
		t.Errorf("Voice = %v, want nova", receivedReq.Voice)
	}

	if receivedReq.ResponseFormat != "opus" {
		t.Errorf("ResponseFormat = %v, want opus", receivedReq.ResponseFormat)
	}

	if receivedReq.Speed != 1.5 {
		t.Errorf("Speed = %v, want 1.5", receivedReq.Speed)
	}

	if receivedReq.Model != ModelTTS1HD {
		t.Errorf("Model = %v, want %v", receivedReq.Model, ModelTTS1HD)
	}
}

// Helper to check error types
func isError(err error, target interface{}) bool {
	switch t := target.(type) {
	case **SynthesisError:
		e, ok := err.(*SynthesisError)
		if ok {
			*t = e
		}
		return ok
	default:
		return false
	}
}
