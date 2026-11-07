package guardrails

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A max_sentences guardrail that fires blocks the reply with its message. Its
// max_sentences param normalizes to max, which the length check's truncation
// also reads: before truncation was limited to the length check, a reply over
// the cap was cut to `max` bytes and shipped as a fragment.
func TestMaxSentencesGuardrail_BlocksRatherThanTruncates(t *testing.T) {
	hook, err := NewGuardrailHook("max_sentences",
		map[string]any{"max_sentences": 2, "min_score": 0.01}, WithMessage("too long"))
	if err != nil {
		t.Fatal(err)
	}
	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "One. Two. Three. Four."}}
	d := hook.AfterCall(context.Background(), &hooks.ProviderRequest{}, resp)
	if d.Allow {
		t.Fatal("guardrail did not fire on four sentences over max 2")
	}
	if resp.Message.Content != "too long" {
		t.Fatalf("content = %q, want the block message", resp.Message.Content)
	}

	ok := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "One. Two."}}
	if d := hook.AfterCall(context.Background(), &hooks.ProviderRequest{}, ok); !d.Allow {
		t.Fatalf("guardrail fired at the cap with min_score 0.01: %+v", d)
	}
}
