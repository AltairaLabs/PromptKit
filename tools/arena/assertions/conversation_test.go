package assertions

import (
	"context"
	"testing"
)

type dummyConvValidator struct{}

func (d *dummyConvValidator) Type() string { return "dummy_validator" }

func (d *dummyConvValidator) ValidateConversation(ctx context.Context, convCtx *ConversationContext, params map[string]interface{}) ConversationValidationResult {
	return ConversationValidationResult{Passed: true, Message: "ok"}
}

func newDummyFactory() ConversationValidator { return &dummyConvValidator{} }

func TestConversationRegistry_RegisterAndGet(t *testing.T) {
	r := NewConversationAssertionRegistry()

	// Ensure unknown returns error
	if _, err := r.Get("not_registered"); err == nil {
		t.Fatalf("expected error for unknown validator")
	}

	// Register dummy and retrieve
	r.Register("dummy_validator", newDummyFactory)
	v, err := r.Get("dummy_validator")
	if err != nil {
		t.Fatalf("unexpected error getting validator: %v", err)
	}
	if v.Type() != "dummy_validator" {
		t.Fatalf("unexpected type: %s", v.Type())
	}

	// Validate convenience method
	conv := &ConversationContext{AllTurns: nil}
	res := r.ValidateConversation(context.Background(), ConversationAssertion{Type: "dummy_validator"}, conv)
	if !res.Passed {
		t.Fatalf("expected validation to pass")
	}
}

func TestConversationRegistry_TypesAndHas(t *testing.T) {
	r := NewConversationAssertionRegistry()
	if r.Has("dummy_validator") {
		t.Fatalf("should not have dummy before register")
	}

	r.Register("dummy_validator", newDummyFactory)
	if !r.Has("dummy_validator") {
		t.Fatalf("expected Has to be true after register")
	}

	list := r.Types()
	found := false
	for _, n := range list {
		if n == "dummy_validator" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Types to include dummy_validator")
	}
}
