package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func TestMarkdown_ConversationAssertionsSection(t *testing.T) {
	tmp := t.TempDir()
	repo := NewMarkdownResultRepository(tmp)

	rr := engine.RunResult{
		RunID:      "r1",
		ScenarioID: "scenario-A",
		ProviderID: "prov",
		Region:     "us",
		Duration:   time.Second,
		ConversationAssertions: engine.AssertionsSummary{
			Failed: 1,
			Passed: false,
			Total:  2,
			Results: []assertions.ConversationValidationResult{
				{Type: "content_not_includes", Passed: false, Message: "forbidden content detected"},
				{Type: "tools_called", Passed: true, Message: "ok"},
			},
		},
	}

	if err := repo.SaveResults([]engine.RunResult{rr}); err != nil {
		t.Fatalf("SaveResults error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "results.md"))
	if err != nil {
		t.Fatalf("read results.md: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "## 🧩 Conversation Assertions") {
		t.Fatalf("expected Conversation Assertions section in markdown")
	}
	if !strings.Contains(s, "scenario-A → prov (us)") {
		t.Fatalf("expected run header in section")
	}
	if !strings.Contains(s, "Failed") {
		t.Fatalf("expected failed status for conversation assertions")
	}
	if !strings.Contains(s, "forbidden content detected") {
		t.Fatalf("expected assertion result rows in table")
	}
}

func TestMarkdown_FormatConversationAssertionDetails(t *testing.T) {
	tests := []struct {
		name    string
		details map[string]interface{}
		want    string
	}{
		{"empty", map[string]interface{}{}, "-"},
		{"scoreOnly", map[string]interface{}{"score": 0.9}, "score=0.9"},
		{"reasoning", map[string]interface{}{"reasoning": "Clear and concise"}, "Clear and concise"},
		{
			name: "scoreReasonTruncate",
			details: map[string]interface{}{
				"score":     0.5,
				"reasoning": "Long reasoning message that should be truncated because it exceeds the limit set by truncate function for markdown outputs.",
			},
			want: "score=0.5 · Long reasoning message that should be truncated because it exceeds the limit set by truncate function for markdown ou...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatConversationAssertionDetails(tt.details); got != tt.want {
				t.Fatalf("formatConversationAssertionDetails() = %q, want %q", got, tt.want)
			}
		})
	}
}
