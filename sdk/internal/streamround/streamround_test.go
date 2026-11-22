package streamround

import "testing"

func TestReplyText(t *testing.T) {
	prev := replyText
	t.Cleanup(func() { replyText = prev })

	replyText = nil
	if got := ReplyText("chunk"); got != "" {
		t.Fatalf("with no lookup registered, ReplyText = %q, want empty", got)
	}

	Register(func(chunk any) string { return "text of " + chunk.(string) })
	if got := ReplyText("chunk"); got != "text of chunk" {
		t.Fatalf("ReplyText = %q, want the registered lookup's %q", got, "text of chunk")
	}
}
