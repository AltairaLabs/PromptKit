package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewCartesia(t *testing.T) {
	service := NewCartesia("test-key")
	if service == nil {
		t.Fatal("NewCartesia() returned nil")
	}

	if service.apiKey != "test-key" {
		t.Errorf("apiKey = %v, want test-key", service.apiKey)
	}

	if service.baseURL != cartesiaBaseURL {
		t.Errorf("baseURL = %v, want %v", service.baseURL, cartesiaBaseURL)
	}

	if service.model != CartesiaModelSonic {
		t.Errorf("model = %v, want %v", service.model, CartesiaModelSonic)
	}
}

func TestNewCartesia_WithOptions(t *testing.T) {
	customClient := &http.Client{}
	service := NewCartesia("test-key",
		WithCartesiaBaseURL("https://custom.api.com"),
		WithCartesiaWSURL("wss://custom.ws.com"),
		WithCartesiaClient(customClient),
		WithCartesiaModel("custom-model"),
	)

	if service.baseURL != "https://custom.api.com" {
		t.Errorf("baseURL = %v, want https://custom.api.com", service.baseURL)
	}

	if service.wsURL != "wss://custom.ws.com" {
		t.Errorf("wsURL = %v, want wss://custom.ws.com", service.wsURL)
	}

	if service.client != customClient {
		t.Error("client was not set correctly")
	}

	if service.model != "custom-model" {
		t.Errorf("model = %v, want custom-model", service.model)
	}
}

func TestCartesiaService_Name(t *testing.T) {
	service := NewCartesia("test-key")
	if service.Name() != "cartesia" {
		t.Errorf("Name() = %v, want cartesia", service.Name())
	}
}

func TestCartesiaService_Synthesize_EmptyText(t *testing.T) {
	service := NewCartesia("test-key")
	_, err := service.Synthesize(context.Background(), "", SynthesisConfig{})
	if err != ErrEmptyText {
		t.Errorf("Synthesize() error = %v, want ErrEmptyText", err)
	}
}

func TestCartesiaService_Synthesize_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("Method = %v, want POST", r.Method)
		}

		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "test-key" {
			t.Errorf("X-API-Key = %v, want test-key", apiKey)
		}

		version := r.Header.Get("Cartesia-Version")
		if version != "2024-06-10" {
			t.Errorf("Cartesia-Version = %v, want 2024-06-10", version)
		}

		// Verify body
		var req cartesiaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Transcript != "Hello world" {
			t.Errorf("Transcript = %v, want Hello world", req.Transcript)
		}

		if req.Voice.Mode != "id" {
			t.Errorf("Voice.Mode = %v, want id", req.Voice.Mode)
		}

		// Return mock audio
		w.Header().Set("Content-Type", "audio/pcm")
		w.Write([]byte("mock audio data"))
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

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

func TestCartesiaService_Synthesize_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "invalid_request",
			"message": "Invalid voice ID",
		})
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

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

func TestCartesiaService_SupportedVoices(t *testing.T) {
	service := NewCartesia("test-key")
	voices := service.SupportedVoices()

	if len(voices) < 3 {
		t.Errorf("len(SupportedVoices()) = %v, want >= 3", len(voices))
	}
}

func TestCartesiaService_SupportedFormats(t *testing.T) {
	service := NewCartesia("test-key")
	formats := service.SupportedFormats()

	if len(formats) < 3 {
		t.Errorf("len(SupportedFormats()) = %v, want >= 3", len(formats))
	}
}

func TestCartesiaService_mapFormat(t *testing.T) {
	service := NewCartesia("test-key")

	tests := []struct {
		format   AudioFormat
		wantEnc  string
		wantCont string
	}{
		{FormatPCM16, "pcm_s16le", "raw"},
		{FormatMP3, "mp3", "mp3"},
		{FormatWAV, "pcm_s16le", "wav"},
		{AudioFormat{Name: "unknown"}, "pcm_s16le", "raw"},
	}

	for _, tt := range tests {
		t.Run(tt.format.Name, func(t *testing.T) {
			result := service.mapFormat(tt.format)
			if result.Encoding != tt.wantEnc {
				t.Errorf("mapFormat(%v).Encoding = %v, want %v", tt.format.Name, result.Encoding, tt.wantEnc)
			}
			if result.Container != tt.wantCont {
				t.Errorf("mapFormat(%v).Container = %v, want %v", tt.format.Name, result.Container, tt.wantCont)
			}
		})
	}
}

func TestCartesiaService_Synthesize_WithDefaultVoice(t *testing.T) {
	var receivedReq cartesiaRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

	// Use empty voice to test default
	reader, err := service.Synthesize(context.Background(), "Test", SynthesisConfig{})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	reader.Close()

	// Should use Barbershop Man as default
	if receivedReq.Voice.ID != "a0e99841-438c-4a64-b679-ae501e7d6091" {
		t.Errorf("Voice.ID = %v, want default voice ID", receivedReq.Voice.ID)
	}
}

func TestCartesiaService_Synthesize_WithLanguage(t *testing.T) {
	var receivedReq cartesiaRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

	reader, err := service.Synthesize(context.Background(), "Bonjour", SynthesisConfig{
		Language: "fr",
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	reader.Close()

	if receivedReq.Language != "fr" {
		t.Errorf("Language = %v, want fr", receivedReq.Language)
	}
}

func TestCartesiaService_processWSResponse(t *testing.T) {
	service := NewCartesia("test-key")

	tests := []struct {
		name      string
		resp      *cartesiaWSResponse
		index     int
		wantChunk bool
		wantErr   bool
		wantData  []byte
		wantFinal bool
	}{
		{
			name: "valid chunk",
			resp: &cartesiaWSResponse{
				Type: "chunk",
				Data: "aGVsbG8=", // base64 "hello"
				Done: false,
			},
			index:     0,
			wantChunk: true,
			wantErr:   false,
			wantData:  []byte("hello"),
			wantFinal: false,
		},
		{
			name: "final chunk",
			resp: &cartesiaWSResponse{
				Type: "chunk",
				Data: "d29ybGQ=", // base64 "world"
				Done: true,
			},
			index:     5,
			wantChunk: true,
			wantErr:   false,
			wantData:  []byte("world"),
			wantFinal: true,
		},
		{
			name: "error response",
			resp: &cartesiaWSResponse{
				Error: "synthesis failed",
			},
			index:     0,
			wantChunk: false,
			wantErr:   true,
		},
		{
			name: "non-chunk type",
			resp: &cartesiaWSResponse{
				Type: "metadata",
			},
			index:     0,
			wantChunk: false,
			wantErr:   false,
		},
		{
			name: "empty data",
			resp: &cartesiaWSResponse{
				Type: "chunk",
				Data: "",
			},
			index:     0,
			wantChunk: false,
			wantErr:   false,
		},
		{
			name: "invalid base64",
			resp: &cartesiaWSResponse{
				Type: "chunk",
				Data: "not-valid-base64!!",
			},
			index:     0,
			wantChunk: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk, err := service.processWSResponse(tt.resp, tt.index)

			if tt.wantErr && err == nil {
				t.Error("processWSResponse() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("processWSResponse() unexpected error: %v", err)
			}

			if tt.wantChunk {
				if chunk == nil {
					t.Fatal("processWSResponse() expected chunk, got nil")
				}
				if string(chunk.Data) != string(tt.wantData) {
					t.Errorf("chunk.Data = %v, want %v", string(chunk.Data), string(tt.wantData))
				}
				if chunk.Index != tt.index {
					t.Errorf("chunk.Index = %v, want %v", chunk.Index, tt.index)
				}
				if chunk.Final != tt.wantFinal {
					t.Errorf("chunk.Final = %v, want %v", chunk.Final, tt.wantFinal)
				}
			} else if chunk != nil && !tt.wantErr {
				t.Errorf("processWSResponse() got chunk, want nil")
			}
		})
	}
}

func TestCartesiaService_handleError_ErrorCases(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		respBody   map[string]interface{}
		wantRetry  bool
	}{
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			respBody:   map[string]interface{}{"error": "rate_limited", "message": "Too many requests"},
			wantRetry:  true,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			respBody:   map[string]interface{}{"error": "server_error", "message": "Internal error"},
			wantRetry:  true,
		},
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
			respBody:   map[string]interface{}{"error": "bad_request", "message": "Invalid request"},
			wantRetry:  false,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			respBody:   map[string]interface{}{"error": "unauthorized", "message": "Invalid API key"},
			wantRetry:  false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			respBody:   map[string]interface{}{"error": "not_found", "message": "Voice not found"},
			wantRetry:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(tt.respBody)
			}))
			defer server.Close()

			testService := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

			_, err := testService.Synthesize(context.Background(), "test", SynthesisConfig{})
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var synthErr *SynthesisError
			if !isError(err, &synthErr) {
				t.Fatalf("expected SynthesisError, got %T", err)
			}

			if synthErr.Retryable != tt.wantRetry {
				t.Errorf("Retryable = %v, want %v", synthErr.Retryable, tt.wantRetry)
			}
		})
	}
}

func TestCartesiaService_handleError_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

	_, err := service.Synthesize(context.Background(), "test", SynthesisConfig{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var synthErr *SynthesisError
	if !isError(err, &synthErr) {
		t.Fatalf("expected SynthesisError, got %T", err)
	}
}

func TestCartesiaService_Synthesize_WithModel(t *testing.T) {
	var receivedReq cartesiaRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

	reader, err := service.Synthesize(context.Background(), "Test", SynthesisConfig{
		Model: "custom-model",
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	reader.Close()

	if receivedReq.ModelID != "custom-model" {
		t.Errorf("ModelID = %v, want custom-model", receivedReq.ModelID)
	}
}

func TestCartesiaService_Synthesize_WithFormat(t *testing.T) {
	var receivedReq cartesiaRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

	// Test with MP3 format
	reader, err := service.Synthesize(context.Background(), "Test", SynthesisConfig{
		Format: FormatMP3,
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	reader.Close()

	if receivedReq.OutputFormat.Encoding != "mp3" {
		t.Errorf("OutputFormat.Encoding = %v, want mp3", receivedReq.OutputFormat.Encoding)
	}
	if receivedReq.OutputFormat.Container != "mp3" {
		t.Errorf("OutputFormat.Container = %v, want mp3", receivedReq.OutputFormat.Container)
	}
}

func TestCartesiaService_Synthesize_WithWAVFormat(t *testing.T) {
	var receivedReq cartesiaRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

	reader, err := service.Synthesize(context.Background(), "Test", SynthesisConfig{
		Format: FormatWAV,
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	reader.Close()

	if receivedReq.OutputFormat.Encoding != "pcm_s16le" {
		t.Errorf("OutputFormat.Encoding = %v, want pcm_s16le", receivedReq.OutputFormat.Encoding)
	}
	if receivedReq.OutputFormat.Container != "wav" {
		t.Errorf("OutputFormat.Container = %v, want wav", receivedReq.OutputFormat.Container)
	}
}

func TestCartesiaService_mapFormat_AllFormats(t *testing.T) {
	service := NewCartesia("test-key")

	tests := []struct {
		name     string
		format   AudioFormat
		wantEnc  string
		wantCont string
		wantRate int
	}{
		{
			name:     "pcm format",
			format:   FormatPCM16,
			wantEnc:  "pcm_s16le",
			wantCont: "raw",
			wantRate: 24000,
		},
		{
			name:     "mp3 format",
			format:   FormatMP3,
			wantEnc:  "mp3",
			wantCont: "mp3",
			wantRate: 44100,
		},
		{
			name:     "wav format",
			format:   FormatWAV,
			wantEnc:  "pcm_s16le",
			wantCont: "wav",
			wantRate: 44100,
		},
		{
			name:     "unknown format defaults to pcm",
			format:   AudioFormat{Name: "flac"},
			wantEnc:  "pcm_s16le",
			wantCont: "raw",
			wantRate: 24000,
		},
		{
			name:     "empty format defaults to pcm",
			format:   AudioFormat{},
			wantEnc:  "pcm_s16le",
			wantCont: "raw",
			wantRate: 24000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.mapFormat(tt.format)
			if result.Encoding != tt.wantEnc {
				t.Errorf("Encoding = %v, want %v", result.Encoding, tt.wantEnc)
			}
			if result.Container != tt.wantCont {
				t.Errorf("Container = %v, want %v", result.Container, tt.wantCont)
			}
			if result.SampleRate != tt.wantRate {
				t.Errorf("SampleRate = %v, want %v", result.SampleRate, tt.wantRate)
			}
		})
	}
}
