package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

func TestNewOpenAI(t *testing.T) {
	service := NewOpenAI("test-key")
	if service == nil {
		t.Fatal("NewOpenAI() returned nil")
	}

	if service.APIKey != "test-key" {
		t.Errorf("APIKey = %v, want test-key", service.APIKey)
	}

	if service.BaseURL != openAIBaseURL {
		t.Errorf("BaseURL = %v, want %v", service.BaseURL, openAIBaseURL)
	}

	if service.Model != ModelTTS1 {
		t.Errorf("Model = %v, want %v", service.Model, ModelTTS1)
	}
}

func TestNewOpenAI_WithOptions(t *testing.T) {
	customClient := &http.Client{}
	service := NewOpenAI("test-key",
		base.WithBaseURL("https://custom.api.com"),
		base.WithClient(customClient),
		base.WithModel(ModelTTS1HD),
	)

	if service.BaseURL != "https://custom.api.com" {
		t.Errorf("BaseURL = %v, want https://custom.api.com", service.BaseURL)
	}

	if service.Client != customClient {
		t.Error("Client was not set correctly")
	}

	if service.Model != ModelTTS1HD {
		t.Errorf("Model = %v, want %v", service.Model, ModelTTS1HD)
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

	service := NewOpenAI("test-key", base.WithBaseURL(server.URL))

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

	service := NewOpenAI("test-key", base.WithBaseURL(server.URL))

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

func TestOpenAIService_Synthesize_LowersMarkupToInstructions_GPT4oMiniTTS(t *testing.T) {
	// Bracket tags on the expressive model should land in `instructions`,
	// and the spoken text in `input` should be the stripped version.
	var got openAIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewOpenAI("k", base.WithBaseURL(server.URL))
	reader, err := service.Synthesize(context.Background(),
		"[whispers]Come here[/]Did you hear that?",
		SynthesisConfig{Voice: VoiceAlloy, Model: ModelGPT4oMiniTTS},
	)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	defer reader.Close()
	_, _ = io.ReadAll(reader)

	if got.Input != "Come hereDid you hear that?" {
		t.Errorf("Input = %q, want stripped spoken text", got.Input)
	}
	if got.Instructions != "whisper" {
		t.Errorf("Instructions = %q, want %q", got.Instructions, "whisper")
	}
	if got.Model != ModelGPT4oMiniTTS {
		t.Errorf("Model = %q, want %q", got.Model, ModelGPT4oMiniTTS)
	}
}

func TestOpenAIService_Synthesize_StripsMarkup_TTS1(t *testing.T) {
	// On tts-1, tags should still be stripped from `input` (so the model
	// doesn't literally speak "[whispers]"), but `instructions` MUST be
	// omitted because the model doesn't support it.
	var got openAIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewOpenAI("k", base.WithBaseURL(server.URL))
	reader, err := service.Synthesize(context.Background(),
		"[excited]Surprise!",
		SynthesisConfig{Voice: VoiceAlloy, Model: ModelTTS1},
	)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	defer reader.Close()
	_, _ = io.ReadAll(reader)

	if got.Input != "Surprise!" {
		t.Errorf("Input = %q, want stripped %q", got.Input, "Surprise!")
	}
	if got.Instructions != "" {
		t.Errorf("Instructions should be empty on tts-1, got %q", got.Instructions)
	}
}

func TestOpenAIService_Synthesize_PlainTextUnchanged(t *testing.T) {
	// No tags ⇒ input is byte-identical to what callers passed in and
	// `instructions` is omitted, so the JSON cache key is unchanged.
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewOpenAI("k", base.WithBaseURL(server.URL))
	reader, err := service.Synthesize(context.Background(),
		"Plain text without any tags.",
		SynthesisConfig{Voice: VoiceAlloy, Model: ModelGPT4oMiniTTS},
	)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	defer reader.Close()
	_, _ = io.ReadAll(reader)

	if strings.Contains(string(raw), `"instructions"`) {
		t.Errorf("instructions field should be omitted for plain text; got body %s", raw)
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

	service := NewOpenAI("test-key", base.WithBaseURL(server.URL))

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

func TestOpenAIService_Pricing(t *testing.T) {
	svc := NewOpenAI("test-key")
	desc := svc.Pricing()
	if desc == nil {
		t.Fatal("Pricing() returned nil descriptor")
	}
	if len(desc.Items) == 0 {
		t.Error("Pricing() descriptor has no PriceItems")
	}
	for _, item := range desc.Items {
		if item.Unit == "" {
			t.Error("PriceItem has empty Unit")
		}
		if item.Rate <= 0 {
			t.Errorf("PriceItem %q has non-positive rate: %v", item.Unit, item.Rate)
		}
	}
}

func TestOpenAIService_ImplName(t *testing.T) {
	svc := NewOpenAI("test-key")
	if got := svc.ImplName(); got != "openai" {
		t.Errorf("ImplName() = %q, want %q", got, "openai")
	}
}

func TestOpenAIService_ModelName(t *testing.T) {
	svc := NewOpenAI("test-key")
	if got := svc.ModelName(); got != ModelTTS1 {
		t.Errorf("ModelName() = %q, want %q", got, ModelTTS1)
	}
	svc2 := NewOpenAI("test-key", base.WithModel(ModelTTS1HD))
	if got := svc2.ModelName(); got != ModelTTS1HD {
		t.Errorf("ModelName() = %q, want %q after WithOpenAIModel", got, ModelTTS1HD)
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
