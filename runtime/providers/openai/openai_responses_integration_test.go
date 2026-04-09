package openai

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestBuildResponsesRequest_ReasoningEffort verifies that reasoning_effort
// configured via additional_config is sent as reasoning.effort in Responses
// API requests. Regression: gpt-5-pro defaults to reasoning.effort=high on
// the server side, which on simple prompts burns 20+ seconds of silent
// reasoning — configurable effort lets callers opt out.
func TestBuildResponsesRequest_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		want   string // "" means the "reasoning" key should be absent
	}{
		{
			name:   "absent when not configured",
			config: nil,
			want:   "",
		},
		{
			name:   "absent when empty",
			config: map[string]any{"reasoning_effort": ""},
			want:   "",
		},
		{
			name:   "absent on unknown value",
			config: map[string]any{"reasoning_effort": "aggressive"},
			want:   "",
		},
		{
			name:   "minimal",
			config: map[string]any{"reasoning_effort": "minimal"},
			want:   "minimal",
		},
		{
			name:   "low",
			config: map[string]any{"reasoning_effort": "low"},
			want:   "low",
		},
		{
			name:   "medium",
			config: map[string]any{"reasoning_effort": "medium"},
			want:   "medium",
		},
		{
			name:   "high",
			config: map[string]any{"reasoning_effort": "high"},
			want:   "high",
		},
		{
			name:   "normalizes case",
			config: map[string]any{"reasoning_effort": "MINIMAL"},
			want:   "minimal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProviderWithConfig(
				"test", "gpt-5-pro", "https://api.openai.com/v1",
				providers.ProviderDefaults{}, false, tt.config,
			)
			req := providers.PredictionRequest{
				Messages: []types.Message{{Role: "user", Content: "hi"}},
			}

			result := provider.buildResponsesRequest(req, nil, "")
			reasoning, present := result["reasoning"]

			if tt.want == "" {
				if present {
					t.Errorf("expected no reasoning field, got %v", reasoning)
				}
				return
			}
			if !present {
				t.Fatalf("expected reasoning.effort=%q, but reasoning field missing", tt.want)
			}
			m, ok := reasoning.(map[string]any)
			if !ok {
				t.Fatalf("reasoning field is %T, want map[string]any", reasoning)
			}
			if got := m["effort"]; got != tt.want {
				t.Errorf("reasoning.effort = %v, want %q", got, tt.want)
			}
		})
	}
}

// TestGetReasoningEffort_DirectParsing covers getReasoningEffort's behavior
// independent of the request builder.
func TestGetReasoningEffort_DirectParsing(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"nil config", nil, ""},
		{"missing key", map[string]any{"other": "x"}, ""},
		{"wrong type", map[string]any{"reasoning_effort": 42}, ""},
		{"empty string", map[string]any{"reasoning_effort": ""}, ""},
		{"unknown value", map[string]any{"reasoning_effort": "garbage"}, ""},
		{"minimal", map[string]any{"reasoning_effort": "minimal"}, "minimal"},
		{"low", map[string]any{"reasoning_effort": "low"}, "low"},
		{"medium", map[string]any{"reasoning_effort": "medium"}, "medium"},
		{"high", map[string]any{"reasoning_effort": "high"}, "high"},
		{"uppercase", map[string]any{"reasoning_effort": "HIGH"}, "high"},
		{"mixed case", map[string]any{"reasoning_effort": "Low"}, "low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getReasoningEffort(tt.cfg)
			if got != tt.want {
				t.Errorf("getReasoningEffort(%v) = %q, want %q", tt.cfg, got, tt.want)
			}
		})
	}
}

func TestHandleStreamEvent_AudioDelta(t *testing.T) {
	provider := NewProviderWithConfig(
		"test", "gpt-4o-audio-preview", "http://localhost",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "responses"},
	)

	outChan := make(chan providers.StreamChunk, 10)
	var sb strings.Builder
	idMap := make(itemIDMap)

	audioB64 := base64.StdEncoding.EncodeToString([]byte("raw-pcm-bytes"))
	data := fmt.Sprintf(`{"type":"response.audio.delta","delta":"%s"}`, audioB64)

	tokens, tc, usage := provider.handleStreamEvent(
		responsesStreamEvent{Type: "response.audio.delta"},
		data, &sb, 0, nil, nil, outChan, idMap,
	)

	if tokens != 0 {
		t.Errorf("tokens = %d, want 0", tokens)
	}
	if tc != nil {
		t.Errorf("unexpected tool calls")
	}
	if usage != nil {
		t.Errorf("unexpected usage")
	}

	select {
	case chunk := <-outChan:
		if chunk.MediaData == nil {
			t.Fatal("expected MediaData in chunk")
		}
		if !bytes.Equal(chunk.MediaData.Data, []byte("raw-pcm-bytes")) {
			t.Errorf("MediaData.Data mismatch")
		}
		if chunk.MediaData.MIMEType != "audio/pcm" {
			t.Errorf("MIMEType = %q, want audio/pcm", chunk.MediaData.MIMEType)
		}
	default:
		t.Fatal("no chunk emitted for audio delta")
	}
}

func TestHandleStreamEvent_AudioTranscriptDelta(t *testing.T) {
	provider := NewProviderWithConfig(
		"test", "gpt-4o-audio-preview", "http://localhost",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "responses"},
	)

	outChan := make(chan providers.StreamChunk, 10)
	var sb strings.Builder
	idMap := make(itemIDMap)

	data := `{"type":"response.audio_transcript.delta","delta":"Hello "}`

	tokens, _, _ := provider.handleStreamEvent(
		responsesStreamEvent{Type: "response.audio_transcript.delta"},
		data, &sb, 0, nil, nil, outChan, idMap,
	)

	if tokens != 1 {
		t.Errorf("tokens = %d, want 1", tokens)
	}

	select {
	case chunk := <-outChan:
		if chunk.Delta != "Hello " {
			t.Errorf("Delta = %q, want %q", chunk.Delta, "Hello ")
		}
		if chunk.Content != "Hello " {
			t.Errorf("Content = %q, want accumulated text", chunk.Content)
		}
	default:
		t.Fatal("no chunk emitted for transcript delta")
	}
}

func TestHandleStreamEvent_AudioDone(t *testing.T) {
	provider := NewProviderWithConfig(
		"test", "gpt-4o-audio-preview", "http://localhost",
		providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "responses"},
	)

	outChan := make(chan providers.StreamChunk, 10)
	var sb strings.Builder
	idMap := make(itemIDMap)

	data := `{"type":"response.audio.done"}`

	tokens, tc, usage := provider.handleStreamEvent(
		responsesStreamEvent{Type: "response.audio.done"},
		data, &sb, 5, nil, nil, outChan, idMap,
	)

	if tokens != 5 {
		t.Errorf("tokens = %d, want 5 (unchanged)", tokens)
	}
	if tc != nil || usage != nil {
		t.Error("unexpected side effects")
	}

	select {
	case <-outChan:
		t.Fatal("unexpected chunk emitted for audio done")
	default:
		// expected — no chunk
	}
}
