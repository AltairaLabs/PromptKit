package selfplay

import (
	"testing"
)

func TestDetectAndStripCompletion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantText string
		wantDet  bool
	}{
		{
			name:     "marker at end",
			input:    "Thanks for the help! [CONVERSATION_COMPLETE]",
			wantText: "Thanks for the help!",
			wantDet:  true,
		},
		{
			name:     "marker in middle",
			input:    "All done [CONVERSATION_COMPLETE] goodbye",
			wantText: "All done  goodbye",
			wantDet:  true,
		},
		{
			name:     "no marker",
			input:    "Just a normal message.",
			wantText: "Just a normal message.",
			wantDet:  false,
		},
		{
			name:     "marker with surrounding whitespace",
			input:    "  [CONVERSATION_COMPLETE]  ",
			wantText: "",
			wantDet:  true,
		},
		{
			name:     "empty string",
			input:    "",
			wantText: "",
			wantDet:  false,
		},
		{
			name:     "marker only",
			input:    "[CONVERSATION_COMPLETE]",
			wantText: "",
			wantDet:  true,
		},
		{
			name:     "multiple markers",
			input:    "[CONVERSATION_COMPLETE] text [CONVERSATION_COMPLETE]",
			wantText: "text",
			wantDet:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detected := DetectAndStripCompletion(tt.input)
			if detected != tt.wantDet {
				t.Errorf("detected = %v, want %v", detected, tt.wantDet)
			}
			if got != tt.wantText {
				t.Errorf("cleaned = %q, want %q", got, tt.wantText)
			}
		})
	}
}
