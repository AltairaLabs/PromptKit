package guardrails

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBannedWordsHook_Name(t *testing.T) {
	h := NewBannedWordsHook([]string{"bad"})
	if h.Name() != "banned_words" {
		t.Errorf("Name() = %q, want %q", h.Name(), "banned_words")
	}
}

func TestBannedWordsHook_BeforeCall(t *testing.T) {
	h := NewBannedWordsHook([]string{"bad"})
	d := h.BeforeCall(context.Background(), &hooks.ProviderRequest{})
	if !d.Allow {
		t.Error("BeforeCall should always allow")
	}
}

func TestBannedWordsHook_AfterCall(t *testing.T) {
	h := NewBannedWordsHook([]string{"forbidden", "blocked"})

	tests := []struct {
		name    string
		content string
		allow   bool
		word    string
	}{
		{"clean content", "This is perfectly fine.", true, ""},
		{"contains forbidden", "This is forbidden content.", false, "forbidden"},
		{"contains blocked", "Something blocked here.", false, "blocked"},
		{"case insensitive", "This is FORBIDDEN content.", false, "forbidden"},
		{"partial word no match", "unforbidden is ok", true, ""},
		{"word boundary", "The word forbidden appears.", false, "forbidden"},
		{"empty content", "", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
				Message: types.Message{Content: tt.content},
			})
			if d.Allow != tt.allow {
				t.Errorf("Allow = %v, want %v", d.Allow, tt.allow)
			}
			if !tt.allow {
				if d.Metadata["validator_type"] != "banned_words" {
					t.Errorf("validator_type = %v, want banned_words", d.Metadata["validator_type"])
				}
				if d.Metadata["violation"] != tt.word {
					t.Errorf("violation = %v, want %q", d.Metadata["violation"], tt.word)
				}
			}
		})
	}
}

func TestBannedWordsHook_OnChunk(t *testing.T) {
	h := NewBannedWordsHook([]string{"secret"})

	tests := []struct {
		name    string
		content string
		allow   bool
	}{
		{"clean chunk", "This is fine so far", true},
		{"chunk with violation", "This contains a secret word", false},
		{"case insensitive chunk", "This has a SECRET", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := h.OnChunk(context.Background(), &providers.StreamChunk{Content: tt.content})
			if d.Allow != tt.allow {
				t.Errorf("Allow = %v, want %v", d.Allow, tt.allow)
			}
		})
	}
}

func TestBannedWordsHook_SpecialCharacters(t *testing.T) {
	h := NewBannedWordsHook([]string{"c++", "c#"})
	// regexp.QuoteMeta ensures special chars are escaped
	d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
		Message: types.Message{Content: "I love programming"},
	})
	if !d.Allow {
		t.Error("should allow content without special-char words")
	}
}

func TestBannedWordsHook_Interfaces(t *testing.T) {
	h := NewBannedWordsHook([]string{"test"})
	// Verify it implements both interfaces
	var _ hooks.ProviderHook = h
	var _ hooks.ChunkInterceptor = h
}
