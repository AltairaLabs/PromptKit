package pages

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

func TestMainPage_Basic(t *testing.T) {
	page := NewMainPage()

	runs := []panels.RunInfo{
		{RunID: "run-1", Scenario: "s1", Provider: "p1", Status: panels.StatusRunning},
	}

	logs := []panels.LogEntry{
		{Level: "INFO", Message: "test"},
	}

	// Basic smoke test
	page.SetDimensions(100, 30)
	page.SetData(runs, logs, "runs", nil)
	view := page.Render()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Active Runs")
}

func TestConversationPage_Basic(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 30)

	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}

	page.SetData("run-1", "scn", "prov", res)
	view := page.Render()
	assert.Contains(t, view, "Conversation")
}
