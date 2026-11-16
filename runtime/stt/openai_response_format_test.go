package stt_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
)

// transcribeServer answers like the transcription API: any response_format
// other than json/text is a 400 naming it, which is what the gpt-4o-*-transcribe
// models return for verbose_json (#2689). On success it returns body.
func transcribeServer(t *testing.T, body map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		if f := r.FormValue("response_format"); f != "" && f != "json" && f != "text" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"code":    "unsupported_value",
				"message": "response_format '" + f + "' is not compatible with model '" + r.FormValue("model") + "'.",
			}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func TestOpenAITranscribe_TranscribeModelAcceptsRequest(t *testing.T) {
	server := transcribeServer(t, map[string]any{"text": "hello from gpt-4o"})
	defer server.Close()

	svc := stt.NewOpenAI("k", base.WithBaseURL(server.URL), base.WithModel("gpt-4o-mini-transcribe"))
	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio:    generateTestAudio(16000, 1.0),
		MIMEType: "audio/pcm",
		Hints:    map[string]string{"sample_rate": "16000"},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if resp.Text != "hello from gpt-4o" {
		t.Errorf("Text = %q, want %q", resp.Text, "hello from gpt-4o")
	}
}

func TestOpenAITranscribe_DurationUsagePricesCompressedAudio(t *testing.T) {
	server := transcribeServer(t, map[string]any{
		"text":  "mp3 speech",
		"usage": map[string]any{"type": "duration", "seconds": 2.5},
	})
	defer server.Close()

	svc := stt.NewOpenAI("k", base.WithBaseURL(server.URL))
	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio:    []byte("not pcm, cannot be estimated"),
		MIMEType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if resp.Cost == nil {
		t.Fatal("Cost = nil, want cost from usage.seconds")
	}
	if got := resp.Cost.Quantities["second"]; got != 2.5 {
		t.Errorf("Quantities[second] = %v, want 2.5", got)
	}
	if want := 2.5 * 0.0001; math.Abs(resp.Cost.TotalCost-want) > 1e-12 {
		t.Errorf("TotalCost = %v, want %v", resp.Cost.TotalCost, want)
	}
}

func TestOpenAITranscribe_TokenUsageReportsTokens(t *testing.T) {
	server := transcribeServer(t, map[string]any{
		"text": "token billed",
		"usage": map[string]any{
			"type":          "tokens",
			"input_tokens":  120,
			"output_tokens": 15,
			"total_tokens":  135,
			"input_token_details": map[string]any{
				"audio_tokens": 100,
				"text_tokens":  20,
			},
		},
	})
	defer server.Close()

	svc := stt.NewOpenAI("k", base.WithBaseURL(server.URL), base.WithModel("gpt-4o-transcribe"))
	svc.SetPricing(&base.PricingDescriptor{
		Source:   base.PricingSourceInline,
		Currency: "usd",
		Items: []base.PriceItem{
			{Unit: base.UnitAudioInputToken, Rate: 0.001},
			{Unit: base.UnitInputToken, Rate: 0.01},
			{Unit: base.UnitOutputToken, Rate: 0.1},
		},
	})
	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio:    generateTestAudio(16000, 1.0),
		MIMEType: "audio/pcm",
		Hints:    map[string]string{"sample_rate": "16000"},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if resp.Cost == nil {
		t.Fatal("Cost = nil, want cost from token usage")
	}
	want := map[string]float64{
		base.UnitAudioInputToken: 100,
		base.UnitInputToken:      20,
		base.UnitOutputToken:     15,
	}
	if len(resp.Cost.Quantities) != len(want) {
		t.Errorf("Quantities = %v, want %v", resp.Cost.Quantities, want)
	}
	for unit, q := range want {
		if got := resp.Cost.Quantities[unit]; got != q {
			t.Errorf("Quantities[%s] = %v, want %v", unit, got, q)
		}
	}
	if wantUSD := 100*0.001 + 20*0.01 + 15*0.1; math.Abs(resp.Cost.TotalCost-wantUSD) > 1e-9 {
		t.Errorf("TotalCost = %v, want %v", resp.Cost.TotalCost, wantUSD)
	}
}

// Without input_token_details every input token is audio: the request's only
// input is the audio file unless a prompt is sent.
func TestOpenAITranscribe_TokenUsageWithoutDetailsIsAudio(t *testing.T) {
	server := transcribeServer(t, map[string]any{
		"text":  "no details",
		"usage": map[string]any{"type": "tokens", "input_tokens": 80, "output_tokens": 4, "total_tokens": 84},
	})
	defer server.Close()

	svc := stt.NewOpenAI("k", base.WithBaseURL(server.URL), base.WithModel("gpt-4o-transcribe"))
	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio:    generateTestAudio(16000, 1.0),
		MIMEType: "audio/pcm",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if resp.Cost == nil {
		t.Fatal("Cost = nil")
	}
	want := map[string]float64{base.UnitAudioInputToken: 80, base.UnitOutputToken: 4}
	if len(resp.Cost.Quantities) != len(want) {
		t.Errorf("Quantities = %v, want %v", resp.Cost.Quantities, want)
	}
	for unit, q := range want {
		if got := resp.Cost.Quantities[unit]; got != q {
			t.Errorf("Quantities[%s] = %v, want %v", unit, got, q)
		}
	}
}
