package assertions

import "testing"

func TestConversationAssertionRegistry_Basic(t *testing.T) {
	r := NewConversationAssertionRegistry()

	// Built-ins should exist
	if !r.Has("tools_called") || !r.Has("content_not_includes") {
		t.Fatalf("expected built-in validators to be registered")
	}

	// Get should return a new instance
	v, err := r.Get("tools_called")
	if err != nil || v == nil {
		t.Fatalf("expected to get a validator, err=%v v=%v", err, v)
	}

	// Register a custom one and validate presence
	r.Register("custom", func() ConversationValidator { return &ContentNotIncludesConversationValidator{} })
	if !r.Has("custom") {
		t.Fatalf("expected custom validator to be registered")
	}

	// Types should include keys
	ts := r.Types()
	if len(ts) == 0 {
		t.Fatalf("expected non-empty types")
	}
}
