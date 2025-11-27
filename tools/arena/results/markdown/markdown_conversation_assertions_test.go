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
