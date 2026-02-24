package guardrails

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestLengthHook_Name(t *testing.T) {
	h := NewLengthHook(100, 0)
	if h.Name() != "length" {
		t.Errorf("Name() = %q, want %q", h.Name(), "length")
	}
}

func TestLengthHook_BeforeCall(t *testing.T) {
	h := NewLengthHook(100, 50)
	d := h.BeforeCall(context.Background(), &hooks.ProviderRequest{})
	if !d.Allow {
		t.Error("BeforeCall should always allow")
	}
}

func TestLengthHook_AfterCall_Characters(t *testing.T) {
	h := NewLengthHook(10, 0) // 10 char limit, no token limit

	tests := []struct {
		name    string
		content string
		allow   bool
	}{
		{"within limit", "short", true},
		{"at limit", "0123456789", true},
		{"over limit", "01234567890", false},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
				Message: types.Message{Content: tt.content},
			})
			if d.Allow != tt.allow {
				t.Errorf("Allow = %v, want %v", d.Allow, tt.allow)
			}
			if !tt.allow && d.Metadata["validator_type"] != "length" {
				t.Errorf("validator_type = %v, want length", d.Metadata["validator_type"])
			}
		})
	}
}

func TestLengthHook_AfterCall_Tokens(t *testing.T) {
	// 4 chars per token estimation, so 100 chars ≈ 25 tokens
	h := NewLengthHook(0, 10) // no char limit, 10 token limit

	short := "short"          // ~1 token
	long := strings.Repeat("a", 44) // 44 chars / 4 = 11 tokens → over limit

	d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
		Message: types.Message{Content: short},
	})
	if !d.Allow {
		t.Error("short content should be within token limit")
	}

	d = h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
		Message: types.Message{Content: long},
	})
	if d.Allow {
		t.Error("long content should exceed token limit")
	}
}

func TestLengthHook_AfterCall_NoLimits(t *testing.T) {
	h := NewLengthHook(0, 0) // no limits
	d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
		Message: types.Message{Content: strings.Repeat("x", 10000)},
	})
	if !d.Allow {
		t.Error("no limits should allow any content")
	}
}

func TestLengthHook_OnChunk(t *testing.T) {
	h := NewLengthHook(20, 0)

	d := h.OnChunk(context.Background(), &providers.StreamChunk{Content: "short"})
	if !d.Allow {
		t.Error("short chunk should be allowed")
	}

	d = h.OnChunk(context.Background(), &providers.StreamChunk{Content: strings.Repeat("x", 21)})
	if d.Allow {
		t.Error("chunk exceeding limit should be denied")
	}
}

func TestLengthHook_OnChunk_UsesActualTokenCount(t *testing.T) {
	h := NewLengthHook(0, 10) // token limit only

	// Short content but high token count from provider
	d := h.OnChunk(context.Background(), &providers.StreamChunk{
		Content:    "hi",
		TokenCount: 15,
	})
	if d.Allow {
		t.Error("should deny when actual token count exceeds limit")
	}

	// Long content but low actual token count
	d = h.OnChunk(context.Background(), &providers.StreamChunk{
		Content:    strings.Repeat("x", 100),
		TokenCount: 5,
	})
	if !d.Allow {
		t.Error("should allow when actual token count is within limit")
	}
}

func TestLengthHook_Interfaces(t *testing.T) {
	h := NewLengthHook(100, 50)
	var _ hooks.ProviderHook = h
	var _ hooks.ChunkInterceptor = h
}
