package types

import "testing"

func TestMessageReasoning_ExcludedFromContent(t *testing.T) {
	txt := "the spoken answer"
	m := Message{
		Role:  "assistant",
		Parts: []ContentPart{{Type: ContentTypeText, Text: &txt}},
		Reasoning: &ReasoningTrace{
			Text:   "internal chain of thought",
			Opaque: []OpaqueReasoning{{Provider: "claude", Kind: "thinking_signature", Data: "abc"}},
		},
	}
	if got := m.GetContent(); got != "the spoken answer" {
		t.Fatalf("GetContent() = %q, reasoning must not appear in content", got)
	}
}

func TestReasoningTrace_ZeroValueIsEmpty(t *testing.T) {
	var rt ReasoningTrace
	if rt.Text != "" || len(rt.Opaque) != 0 || rt.Redacted {
		t.Fatal("zero ReasoningTrace should be empty")
	}
}

// TestReasoning_NotInContentProjection pins the structural guarantee: a
// reasoning-only message projects no content and is not multimodal, so any
// content/Parts-based consumer (stores, exports, request builders) excludes it.
func TestReasoning_NotInContentProjection(t *testing.T) {
	m := Message{Role: "assistant", Reasoning: &ReasoningTrace{Text: "secret reasoning"}}
	if m.GetContent() != "" {
		t.Fatal("a reasoning-only message must project empty content")
	}
	if m.IsMultimodal() {
		t.Fatal("reasoning must not make a message multimodal (it is not a Part)")
	}
}

func TestStripReasoning(t *testing.T) {
	in := []Message{{Role: "assistant", Content: "hi", Reasoning: &ReasoningTrace{Text: "thoughts"}}}
	out := StripReasoning(in)
	if out[0].Reasoning != nil {
		t.Fatal("StripReasoning must clear Reasoning")
	}
	if in[0].Reasoning == nil {
		t.Fatal("StripReasoning must not mutate the input")
	}
}
