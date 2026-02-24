package guardrails

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestMaxSentencesHook_Name(t *testing.T) {
	h := NewMaxSentencesHook(3)
	if h.Name() != "max_sentences" {
		t.Errorf("Name() = %q, want %q", h.Name(), "max_sentences")
	}
}

func TestMaxSentencesHook_BeforeCall(t *testing.T) {
	h := NewMaxSentencesHook(3)
	d := h.BeforeCall(context.Background(), &hooks.ProviderRequest{})
	if !d.Allow {
		t.Error("BeforeCall should always allow")
	}
}

func TestMaxSentencesHook_AfterCall(t *testing.T) {
	h := NewMaxSentencesHook(2) // max 2 sentences

	tests := []struct {
		name    string
		content string
		allow   bool
	}{
		{"one sentence", "Hello world.", true},
		{"two sentences", "Hello. World.", true},
		{"three sentences", "One. Two. Three.", false},
		{"no punctuation", "Hello world", true},
		{"empty", "", true},
		{"exclamation", "Wow! Great!", true},
		{"mixed punctuation", "Hello! How are you? Fine.", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
				Message: types.Message{Content: tt.content},
			})
			if d.Allow != tt.allow {
				t.Errorf("Allow = %v, want %v", d.Allow, tt.allow)
			}
			if !tt.allow && d.Metadata["validator_type"] != "max_sentences" {
				t.Errorf("validator_type = %v, want max_sentences", d.Metadata["validator_type"])
			}
		})
	}
}

func TestMaxSentencesHook_NotStreamable(t *testing.T) {
	h := NewMaxSentencesHook(3)
	// Should NOT implement ChunkInterceptor
	if _, ok := interface{}(h).(hooks.ChunkInterceptor); ok {
		t.Error("MaxSentencesHook should not implement ChunkInterceptor")
	}
}

func TestCountSentences(t *testing.T) {
	tests := []struct {
		text  string
		count int
	}{
		{"", 0},
		{"Hello world", 1},
		{"Hello.", 1},
		{"Hello. World.", 2},
		{"One. Two. Three.", 3},
		{"  ", 0},
		{"Hello! World? Yes.", 3},
		{"Wait... Really?!", 2},
		{"No punctuation here", 1},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := countSentences(tt.text)
			if got != tt.count {
				t.Errorf("countSentences(%q) = %d, want %d", tt.text, got, tt.count)
			}
		})
	}
}
