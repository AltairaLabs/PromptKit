package types

import "testing"

func TestExcludeInterrupted_DropsOnlyInterruptedReplies(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "tell me a story"},
		{Role: "assistant", Content: "Once upon", FinishReason: FinishReasonInterrupted},
		{Role: "user", Content: "what is 2+2?"},
		{Role: "assistant", Content: "Four.", FinishReason: FinishReasonStop},
	}

	got := ExcludeInterrupted(msgs)

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	for _, m := range got {
		if m.IsInterrupted() {
			t.Fatalf("interrupted reply %q kept in model context", m.Content)
		}
	}
	if got[1].Content != "what is 2+2?" || got[2].Content != "Four." {
		t.Fatalf("order not preserved: %+v", got)
	}
	if len(msgs) != 4 || !msgs[1].IsInterrupted() {
		t.Fatal("input slice was modified")
	}
}

func TestExcludeInterrupted_ReturnsInputWhenNoneInterrupted(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	got := ExcludeInterrupted(msgs)
	if len(got) != len(msgs) || &got[0] != &msgs[0] {
		t.Fatal("want the input slice itself when there is nothing to drop")
	}
}
