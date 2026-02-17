package assertions

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestContentMatchesConversationValidator_Type(t *testing.T) {
	v := NewContentMatchesConversationValidator()
	if v.Type() != "content_matches" {
		t.Fatalf("expected type content_matches, got %s", v.Type())
	}
}

func TestContentMatchesConversationValidator_NoPattern(t *testing.T) {
	v := NewContentMatchesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{}

	res := v.ValidateConversation(ctx, conv, map[string]interface{}{})
	if !res.Passed {
		t.Fatalf("expected pass when no pattern specified, got: %+v", res)
	}
}

func TestContentMatchesConversationValidator_Match(t *testing.T) {
	v := NewContentMatchesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "Order ORD-12345 confirmed for $79.99"},
		},
	}

	params := map[string]interface{}{
		"pattern": `ORD-\d+`,
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass, got: %+v", res)
	}
}

func TestContentMatchesConversationValidator_NoMatch(t *testing.T) {
	v := NewContentMatchesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "assistant", Content: "Hello, how can I help?"},
		},
	}

	params := map[string]interface{}{
		"pattern": `ORD-\d+`,
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail, got: %+v", res)
	}
}

func TestContentMatchesConversationValidator_InvalidRegex(t *testing.T) {
	v := NewContentMatchesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{}

	params := map[string]interface{}{
		"pattern": `[invalid`,
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail for invalid regex, got: %+v", res)
	}
}

func TestContentMatchesConversationValidator_SkipsUserMessages(t *testing.T) {
	v := NewContentMatchesConversationValidator()
	ctx := context.Background()
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "ORD-12345"},
		},
	}

	params := map[string]interface{}{
		"pattern": `ORD-\d+`,
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail when only user message matches, got: %+v", res)
	}
}
