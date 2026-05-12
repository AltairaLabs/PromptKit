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
	"github.com/AltairaLabs/PromptKit/runtime/tts/markup"
)

func TestNewElevenLabs(t *testing.T) {
	service := NewElevenLabs("test-key")
	if service == nil {
		t.Fatal("NewElevenLabs() returned nil")
	}

	if service.APIKey != "test-key" {
		t.Errorf("APIKey = %v, want test-key", service.APIKey)
	}

	if service.BaseURL != elevenLabsBaseURL {
		t.Errorf("BaseURL = %v, want %v", service.BaseURL, elevenLabsBaseURL)
	}

	if service.Model != ElevenLabsModelMultilingual {
		t.Errorf("Model = %v, want %v", service.Model, ElevenLabsModelMultilingual)
	}
}

func TestNewElevenLabs_WithOptions(t *testing.T) {
	customClient := &http.Client{}
	service := NewElevenLabs("test-key",
		base.WithBaseURL("https://custom.api.com"),
		base.WithClient(customClient),
		base.WithModel(ElevenLabsModelTurbo),
	)

	if service.BaseURL != "https://custom.api.com" {
		t.Errorf("BaseURL = %v, want https://custom.api.com", service.BaseURL)
	}

	if service.Client != customClient {
		t.Error("Client was not set correctly")
	}

	if service.Model != ElevenLabsModelTurbo {
		t.Errorf("Model = %v, want %v", service.Model, ElevenLabsModelTurbo)
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

	service := NewElevenLabs("test-key", base.WithBaseURL(server.URL))

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

func TestElevenLabsService_Synthesize_V3PassesThroughTags(t *testing.T) {
	// v3 model: the request Text should contain the bracket tags verbatim.
	var got elevenLabsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewElevenLabs("k", base.WithBaseURL(server.URL))
	reader, err := service.Synthesize(context.Background(),
		"[whispers]Come here[/]Did you hear that?",
		SynthesisConfig{Voice: "v", Model: ElevenLabsModelV3},
	)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	defer reader.Close()
	_, _ = io.ReadAll(reader)

	want := "[whispers]Come here[/]Did you hear that?"
	if got.Text != want {
		t.Errorf("v3 should pass tags through verbatim; got Text = %q, want %q", got.Text, want)
	}
}

func TestElevenLabsService_Synthesize_NonV3StripsTags(t *testing.T) {
	// Non-v3 model: brackets get stripped so the model doesn't speak them.
	var got elevenLabsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	service := NewElevenLabs("k", base.WithBaseURL(server.URL))
	reader, err := service.Synthesize(context.Background(),
		"[excited]Surprise!",
		SynthesisConfig{Voice: "v", Model: ElevenLabsModelMultilingual},
	)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	defer reader.Close()
	_, _ = io.ReadAll(reader)

	if got.Text != "Surprise!" {
		t.Errorf("non-v3 should strip tags; got Text = %q, want %q", got.Text, "Surprise!")
	}
}

func TestElevenLabsService_Synthesize_PlainTextUnchanged(t *testing.T) {
	// No tags ⇒ Text is byte-identical regardless of model. Cache key stable.
	for _, model := range []string{ElevenLabsModelV3, ElevenLabsModelMultilingual} {
		var got elevenLabsRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("audio"))
		}))

		service := NewElevenLabs("k", base.WithBaseURL(server.URL))
		reader, err := service.Synthesize(context.Background(),
			"Plain text without any tags.",
			SynthesisConfig{Voice: "v", Model: model},
		)
		if err != nil {
			t.Fatalf("Synthesize(%s): %v", model, err)
		}
		_, _ = io.ReadAll(reader)
		reader.Close()
		server.Close()

		if got.Text != "Plain text without any tags." {
			t.Errorf("model=%s: Text should be unchanged, got %q", model, got.Text)
		}
	}
}

func TestElevenLabsService_PersonaRubric_PerModel(t *testing.T) {
	cases := map[string]bool{
		ElevenLabsModelV3:             true,
		"eleven_v3_alpha":             true,
		ElevenLabsModelMultilingual:   false,
		ElevenLabsModelTurbo:          false,
		ElevenLabsModelEnglish:        false,
		ElevenLabsModelMultilingualV1: false,
	}
	for model, wantRubric := range cases {
		s := NewElevenLabs("k", base.WithModel(model))
		got := s.PersonaRubric()
		if wantRubric {
			if got != markup.RubricExpressiveFull {
				t.Errorf("model=%s: expected RubricExpressiveFull, got len=%d", model, len(got))
			}
		} else if got != "" {
			t.Errorf("model=%s: expected empty rubric, got %q", model, got)
		}
	}
}

func TestElevenLabsSupportsInlineTags(t *testing.T) {
	for in, want := range map[string]bool{
		"eleven_v3":              true,
		"eleven_v3_alpha":        true,
		"eleven_multilingual_v2": false,
		"eleven_turbo_v2_5":      false,
		"":                       false,
	} {
		if got := elevenLabsSupportsInlineTags(in); got != want {
			t.Errorf("elevenLabsSupportsInlineTags(%q) = %v, want %v", in, got, want)
		}
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

	service := NewElevenLabs("test-key", base.WithBaseURL(server.URL))

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

	service := NewElevenLabs("test-key", base.WithBaseURL(server.URL))

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

func TestElevenLabsService_Pricing(t *testing.T) {
	svc := NewElevenLabs("test-key")
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

func TestElevenLabsService_ImplName(t *testing.T) {
	svc := NewElevenLabs("test-key")
	if got := svc.ImplName(); got != "elevenlabs" {
		t.Errorf("ImplName() = %q, want %q", got, "elevenlabs")
	}
}

func TestElevenLabsService_ModelName(t *testing.T) {
	svc := NewElevenLabs("test-key")
	if got := svc.ModelName(); got != ElevenLabsModelMultilingual {
		t.Errorf("ModelName() = %q, want %q", got, ElevenLabsModelMultilingual)
	}
	svc2 := NewElevenLabs("test-key", base.WithModel(ElevenLabsModelTurbo))
	if got := svc2.ModelName(); got != ElevenLabsModelTurbo {
		t.Errorf("ModelName() = %q, want %q after WithElevenLabsModel", got, ElevenLabsModelTurbo)
	}
}
