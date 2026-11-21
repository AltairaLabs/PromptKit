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

// What a caller heard of a spoken reply they cut off (an interrupted reply
// carrying playout_cut, with no tool calls) is what was said, so it stays in
// model context; the model's own unfinished fragment does not.
func TestExcludeInterrupted_KeepsWhatTheCallerHeard(t *testing.T) {
	heard := Message{Role: "assistant", Content: "The fox woke early.", FinishReason: FinishReasonInterrupted,
		Meta: map[string]interface{}{MetaPlayoutCut: PlayoutCutBargeIn}}
	fragment := Message{Role: "assistant", Content: "The fox woke early. It ran to", FinishReason: FinishReasonInterrupted}
	withCalls := heard
	withCalls.ToolCalls = []MessageToolCall{{ID: "c1", Name: "weather"}}
	msgs := []Message{{Role: "user", Content: "story"}, heard, fragment, withCalls}

	got := ExcludeInterrupted(msgs)

	if len(got) != 2 || got[1].Content != "The fox woke early." {
		t.Fatalf("want the user turn and the heard reply, got %+v", got)
	}
	if !heard.IsHeardPrefix() || fragment.IsHeardPrefix() || withCalls.IsHeardPrefix() {
		t.Fatal("only a heard reply without tool calls is a heard prefix")
	}
}
