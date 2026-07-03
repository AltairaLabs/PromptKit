package openai

import (
	"testing"
)

func TestOutputModalities(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty defaults to audio", nil, []string{"audio"}},
		{"empty slice defaults to audio", []string{}, []string{"audio"}},
		{"audio present flattens to audio", []string{"text", "audio"}, []string{"audio"}},
		{"audio only", []string{"audio"}, []string{"audio"}},
		{"text only", []string{"text"}, []string{"text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := outputModalities(tt.in)
			if len(got) != len(tt.want) || (len(got) > 0 && got[0] != tt.want[0]) {
				t.Errorf("outputModalities(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestPcmFormat(t *testing.T) {
	tests := []struct {
		name    string
		legacy  string
		rate    int
		wantNil bool
	}{
		{"empty codec defaults to pcm", "", 24000, false},
		{"pcm16 maps to audio/pcm", "pcm16", 24000, false},
		{"unknown codec yields nil", "g711_ulaw", 8000, true},
		{"another unknown codec yields nil", "opus", 48000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pcmFormat(tt.legacy, tt.rate)
			if tt.wantNil {
				if got != nil {
					t.Errorf("pcmFormat(%q) = %+v, want nil", tt.legacy, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("pcmFormat(%q) = nil, want non-nil", tt.legacy)
			}
			if got.Type != "audio/pcm" {
				t.Errorf("Type = %q, want audio/pcm", got.Type)
			}
			if got.Rate != tt.rate {
				t.Errorf("Rate = %d, want %d", got.Rate, tt.rate)
			}
		})
	}
}

func TestBuildRealtimeSessionConfig_TurnDetectionNilMeansManual(t *testing.T) {
	cfg := RealtimeSessionConfig{
		Modalities:        []string{"audio"},
		Instructions:      "be helpful",
		Voice:             "alloy",
		InputAudioFormat:  "pcm16",
		OutputAudioFormat: "pcm16",
		TurnDetection:     nil, // manual turn control
	}

	got := buildRealtimeSessionConfig(cfg)

	if got.Type != "realtime" {
		t.Errorf("Type = %q, want realtime", got.Type)
	}
	if got.Instructions != "be helpful" {
		t.Errorf("Instructions = %q", got.Instructions)
	}
	if got.Audio == nil || got.Audio.Input == nil {
		t.Fatal("expected audio.input to be set")
	}
	// Nil TurnDetection ⇒ the field stays nil so it marshals as "turn_detection":null
	// (the GA "manual turn control" signal).
	if got.Audio.Input.TurnDetection != nil {
		t.Errorf("TurnDetection = %+v, want nil for manual-turn semantics", got.Audio.Input.TurnDetection)
	}
	if got.Audio.Output == nil || got.Audio.Output.Voice != "alloy" {
		t.Errorf("expected audio.output.voice=alloy")
	}
	if got.Audio.Input.Format == nil || got.Audio.Input.Format.Type != "audio/pcm" {
		t.Errorf("expected pcm input format")
	}
}

func TestBuildRealtimeSessionConfig_VADSetNestsUnderInput(t *testing.T) {
	cfg := RealtimeSessionConfig{
		Modalities: []string{"audio"},
		TurnDetection: &TurnDetectionConfig{
			Type:              "server_vad",
			Threshold:         0.5,
			PrefixPaddingMs:   300,
			SilenceDurationMs: 500,
			CreateResponse:    true,
		},
		InputAudioTranscription: &TranscriptionConfig{Model: "whisper-1"},
	}

	got := buildRealtimeSessionConfig(cfg)

	td := got.Audio.Input.TurnDetection
	if td == nil {
		t.Fatal("expected turn_detection to be nested under audio.input")
	}
	if td.Type != "server_vad" || td.Threshold != 0.5 || td.SilenceDurationMs != 500 {
		t.Errorf("turn_detection not copied faithfully: %+v", td)
	}
	if got.Audio.Input.Transcription == nil || got.Audio.Input.Transcription.Model != "whisper-1" {
		t.Errorf("expected transcription model whisper-1, got %+v", got.Audio.Input.Transcription)
	}
}

func TestBuildRealtimeSessionConfig_ToolsAndMaxTokens(t *testing.T) {
	cfg := RealtimeSessionConfig{
		Modalities: []string{"text"},
		Tools: []RealtimeToolDefinition{
			{
				Type:        "function",
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]any{"type": "object"},
			},
		},
		MaxResponseOutputTokens: 4096,
	}

	got := buildRealtimeSessionConfig(cfg)

	if len(got.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got.Tools))
	}
	if got.Tools[0].Name != "get_weather" || got.Tools[0].Type != "function" {
		t.Errorf("tool mapping wrong: %+v", got.Tools[0])
	}
	if got.MaxOutputTokens != 4096 {
		t.Errorf("MaxOutputTokens = %v, want 4096", got.MaxOutputTokens)
	}
	if len(got.OutputModalities) != 1 || got.OutputModalities[0] != "text" {
		t.Errorf("OutputModalities = %v, want [text]", got.OutputModalities)
	}
}

func TestBuildRealtimeSessionConfig_NoToolsNoMaxTokens(t *testing.T) {
	got := buildRealtimeSessionConfig(RealtimeSessionConfig{Modalities: []string{"audio"}})
	if got.Tools != nil {
		t.Errorf("expected no tools, got %+v", got.Tools)
	}
	if got.MaxOutputTokens != nil {
		t.Errorf("expected no max tokens, got %v", got.MaxOutputTokens)
	}
}

func TestMarkToolCallEmitted_Dedup(t *testing.T) {
	s := &RealtimeSession{}

	// Empty item IDs are never deduped (best-effort emit ⇒ always false).
	if s.markToolCallEmitted("") {
		t.Error("empty item id should never be marked emitted")
	}
	if s.markToolCallEmitted("") {
		t.Error("empty item id should still not dedupe on repeat")
	}

	// First sighting of a real ID returns false (emit); second returns true (skip).
	if s.markToolCallEmitted("item_1") {
		t.Error("first emit of item_1 should return false")
	}
	if !s.markToolCallEmitted("item_1") {
		t.Error("second emit of item_1 should return true (deduped)")
	}
	// Distinct IDs are tracked independently.
	if s.markToolCallEmitted("item_2") {
		t.Error("first emit of item_2 should return false")
	}
}

func TestRealtimeWSURL(t *testing.T) {
	got := realtimeWSURL("wss://api.openai.com/v1/realtime", "gpt-realtime")
	want := "wss://api.openai.com/v1/realtime?model=gpt-realtime"
	if got != want {
		t.Errorf("realtimeWSURL = %q, want %q", got, want)
	}
}

// TestRealtimeWSHeaders_NoBetaHeader is the GA regression guard: the Realtime
// handshake must carry Authorization but must NOT send the legacy OpenAI-Beta
// header (the GA server rejects it and serves pre-GA defaults).
func TestRealtimeWSHeaders_NoBetaHeader(t *testing.T) {
	headers := realtimeWSHeaders("sk-test-123")

	if got := headers.Get("Authorization"); got != "Bearer sk-test-123" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-test-123")
	}
	if v := headers.Get("OpenAI-Beta"); v != "" {
		t.Errorf("OpenAI-Beta must be absent for the GA Realtime API, got %q", v)
	}
	if _, ok := headers["Openai-Beta"]; ok {
		t.Error("OpenAI-Beta header key present, must be absent for GA")
	}
}
