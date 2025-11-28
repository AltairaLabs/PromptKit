package junit

import "testing"

func TestFormatConversationAssertionDetails(t *testing.T) {
	tests := []struct {
		name    string
		details map[string]interface{}
		want    string
	}{
		{"empty", map[string]interface{}{}, ""},
		{"scoreOnly", map[string]interface{}{"score": 0.5}, "score=0.5"},
		{"scoreAndReason", map[string]interface{}{"score": 0.5, "reasoning": "Good"}, "score=0.5 · Good"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatConversationAssertionDetails(tt.details); got != tt.want {
				t.Fatalf("formatConversationAssertionDetails() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateReasoning(t *testing.T) {
	short := truncateReasoning("abc", 10)
	if short != "abc" {
		t.Fatalf("expected short reasoning unchanged, got %q", short)
	}
	truncated := truncateReasoning("abcdefghij", 5)
	if truncated != "ab..." {
		t.Fatalf("expected truncated reasoning, got %q", truncated)
	}
}
