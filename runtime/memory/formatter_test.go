package memory

import "testing"

func TestDefaultContextFormatter_RendersTypeContentConfidence(t *testing.T) {
	got := DefaultContextFormatter([]*Memory{
		{Type: "fact", Content: "User likes Go", Confidence: 0.9},
		{Type: "pref", Content: "Dark mode", Confidence: 0.75},
	})
	want := "[fact] User likes Go (confidence: 0.9)\n" +
		"[pref] Dark mode (confidence: 0.8)\n"
	if got != want {
		t.Errorf("DefaultContextFormatter:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestDefaultContextFormatter_EmptyInput(t *testing.T) {
	if got := DefaultContextFormatter(nil); got != "" {
		t.Errorf("expected empty string for nil memories, got %q", got)
	}
	if got := DefaultContextFormatter([]*Memory{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}
