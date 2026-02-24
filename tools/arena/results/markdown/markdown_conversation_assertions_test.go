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
	if !strings.Contains(s, "| Passed | Type | Score | Message | Details |") {
		t.Fatalf("expected new table header with Type and Score columns")
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
			name:    "durationOnly",
			details: map[string]interface{}{"duration_ms": int64(42)},
			want:    "42ms",
		},
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

func TestMarkdown_ScoreColumnInTable(t *testing.T) {
	tmp := t.TempDir()
	repo := NewMarkdownResultRepository(tmp)

	rr := engine.RunResult{
		RunID:      "r2",
		ScenarioID: "scenario-B",
		ProviderID: "prov",
		Region:     "us",
		Duration:   time.Second,
		ConversationAssertions: engine.AssertionsSummary{
			Failed: 0,
			Passed: true,
			Total:  2,
			Results: []assertions.ConversationValidationResult{
				{
					Type: "pack_eval:llm_judge", Passed: true, Message: "good",
					Details: map[string]interface{}{"score": 0.92, "duration_ms": int64(50)},
				},
				{
					Type: "tools_called", Passed: true, Message: "ok",
				},
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

	// Verify table header has Score column
	if !strings.Contains(s, "| Passed | Type | Score | Message | Details |") {
		t.Fatalf("expected Score column in table header, got:\n%s", s)
	}
	// Verify score value appears
	if !strings.Contains(s, "0.92") {
		t.Fatalf("expected score value 0.92 in output")
	}
	// Verify type column value
	if !strings.Contains(s, "pack_eval:llm_judge") {
		t.Fatalf("expected type column value")
	}
}
