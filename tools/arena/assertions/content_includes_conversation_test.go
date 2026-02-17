package assertions

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestContentIncludesConversationValidator_Type(t *testing.T) {
	v := NewContentIncludesConversationValidator()
	if v.Type() != "content_includes" {
		t.Fatalf("expected type content_includes, got %s", v.Type())
	}
}

func TestContentIncludesConversationValidator_NoPatterns(t *testing.T) {
	v := NewContentIncludesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{}

	res := v.ValidateConversation(ctx, conv, map[string]interface{}{})
	if !res.Passed {
		t.Fatalf("expected pass when no patterns specified, got: %+v", res)
	}
}

func TestContentIncludesConversationValidator_AllPatternsFound(t *testing.T) {
	v := NewContentIncludesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "what's my order?"},
			{Role: "assistant", Content: "Your order ORD-123 total is $25.98. Transaction TXN-456."},
		},
	}

	params := map[string]interface{}{
		"patterns": []interface{}{"ORD-", "TXN-"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass, got: %+v", res)
	}
}

func TestContentIncludesConversationValidator_PartialMatch(t *testing.T) {
	v := NewContentIncludesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "assistant", Content: "Your order ORD-123 is confirmed."},
		},
	}

	params := map[string]interface{}{
		"patterns": []interface{}{"ORD-", "TXN-"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail when not all patterns found, got: %+v", res)
	}
}

func TestContentIncludesConversationValidator_CaseInsensitive(t *testing.T) {
	v := NewContentIncludesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "assistant", Content: "Payment confirmed via PAYPAL"},
		},
	}

	params := map[string]interface{}{
		"patterns": []interface{}{"paypal"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected case-insensitive pass, got: %+v", res)
	}
}

func TestContentIncludesConversationValidator_SkipsUserMessages(t *testing.T) {
	v := NewContentIncludesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "ORD-123 TXN-456"},
		},
	}

	params := map[string]interface{}{
		"patterns": []interface{}{"ORD-", "TXN-"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail when only user message matches, got: %+v", res)
	}
}
