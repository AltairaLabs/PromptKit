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

	// Tool call conversation validators should be registered
	toolCallTypes := []string{
		"no_tool_errors", "tool_call_count", "tool_result_includes",
		"tool_result_matches", "tool_call_sequence", "tool_call_chain",
	}
	for _, typ := range toolCallTypes {
		if !r.Has(typ) {
			t.Errorf("expected %q conversation validator to be registered", typ)
		}
	}
}
