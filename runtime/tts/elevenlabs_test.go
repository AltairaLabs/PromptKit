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

func TestNewElevenLabs(t *testing.T) {
	service := NewElevenLabs("test-key")
	if service == nil {
		t.Fatal("NewElevenLabs() returned nil")
	}

	if service.apiKey != "test-key" {
		t.Errorf("apiKey = %v, want test-key", service.apiKey)
	}

	if service.baseURL != elevenLabsBaseURL {
		t.Errorf("baseURL = %v, want %v", service.baseURL, elevenLabsBaseURL)
	}

	if service.model != ElevenLabsModelMultilingual {
		t.Errorf("model = %v, want %v", service.model, ElevenLabsModelMultilingual)
	}
}

func TestNewElevenLabs_WithOptions(t *testing.T) {
	customClient := &http.Client{}
	service := NewElevenLabs("test-key",
		WithElevenLabsBaseURL("https://custom.api.com"),
		WithElevenLabsClient(customClient),
		WithElevenLabsModel(ElevenLabsModelTurbo),
	)

	if service.baseURL != "https://custom.api.com" {
		t.Errorf("baseURL = %v, want https://custom.api.com", service.baseURL)
	}

	if service.client != customClient {
		t.Error("client was not set correctly")
	}

	if service.model != ElevenLabsModelTurbo {
		t.Errorf("model = %v, want %v", service.model, ElevenLabsModelTurbo)
	}
}

func TestElevenLabsService_Name(t *testing.T) {
	service := NewElevenLabs("test-key")
	if service.Name() != "elevenlabs" {
		t.Errorf("Name() = %v, want elevenlabs", service.Name())
	}
}

func TestElevenLabsService_Synthesize_EmptyText(t *testing.T) {
	service := NewElevenLabs("test-key")
	_, err := service.Synthesize(context.Background(), "", SynthesisConfig{})
	if err != ErrEmptyText {
		t.Errorf("Synthesize() error = %v, want ErrEmptyText", err)
	}
}

func TestElevenLabsService_Synthesize_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("Method = %v, want POST", r.Method)
		}

		if !strings.Contains(r.URL.Path, "/text-to-speech/") {
			t.Errorf("Path = %v, should contain /text-to-speech/", r.URL.Path)
		}

		auth := r.Header.Get("xi-api-key")
		if auth != "test-key" {
			t.Errorf("xi-api-key = %v, want test-key", auth)
		}

		// Verify body
		var req elevenLabsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Text != "Hello world" {
			t.Errorf("Text = %v, want Hello world", req.Text)
		}

		// Return mock audio
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("mock audio data"))
	}))
	defer server.Close()

	service := NewElevenLabs("test-key", WithElevenLabsBaseURL(server.URL))

	reader, err := service.Synthesize(context.Background(), "Hello world", SynthesisConfig{
		Voice: "test-voice-id",
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

func TestElevenLabsService_Synthesize_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"detail": map[string]interface{}{
				"status":  "voice_not_found",
				"message": "Voice not found",
			},
		})
	}))
	defer server.Close()

	service := NewElevenLabs("test-key", WithElevenLabsBaseURL(server.URL))

	_, err := service.Synthesize(context.Background(), "Hello", SynthesisConfig{
		Voice: "invalid-voice",
	})
	if err == nil {
		t.Fatal("Synthesize() should return error")
	}

	var synthErr *SynthesisError
	if !isError(err, &synthErr) {
		t.Fatalf("error should be SynthesisError, got %T", err)
	}
}

func TestElevenLabsService_SupportedVoices(t *testing.T) {
	service := NewElevenLabs("test-key")
	voices := service.SupportedVoices()

	if len(voices) < 5 {
		t.Errorf("len(SupportedVoices()) = %v, want >= 5", len(voices))
	}

	// Verify Rachel is present (default voice)
	found := false
	for _, v := range voices {
		if v.Name == "Rachel" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Rachel voice not found in SupportedVoices()")
	}
}

func TestElevenLabsService_SupportedFormats(t *testing.T) {
	service := NewElevenLabs("test-key")
	formats := service.SupportedFormats()

	if len(formats) < 2 {
		t.Errorf("len(SupportedFormats()) = %v, want >= 2", len(formats))
	}
}

func TestElevenLabsService_mapFormat(t *testing.T) {
	service := NewElevenLabs("test-key")

	tests := []struct {
		format AudioFormat
		want   string
	}{
		{FormatMP3, "mp3_44100_128"},
		{FormatPCM16, "pcm_24000"},
		{AudioFormat{}, "mp3_44100_128"},
		{AudioFormat{Name: "unknown"}, "mp3_44100_128"},
	}

	for _, tt := range tests {
		t.Run(tt.format.Name, func(t *testing.T) {
			if got := service.mapFormat(tt.format); got != tt.want {
				t.Errorf("mapFormat(%v) = %v, want %v", tt.format.Name, got, tt.want)
			}
		})
	}
}

func TestElevenLabsService_Synthesize_WithDefaultVoice(t *testing.T) {
	var requestPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewElevenLabs("test-key", WithElevenLabsBaseURL(server.URL))

	// Use empty voice to test default
	reader, err := service.Synthesize(context.Background(), "Test", SynthesisConfig{})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	reader.Close()

	// Should use Rachel's ID as default
	if !strings.Contains(requestPath, "21m00Tcm4TlvDq8ikWAM") {
		t.Errorf("Path should contain default voice ID, got %v", requestPath)
	}
}
