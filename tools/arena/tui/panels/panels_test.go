package panels

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestConversationPanel_ViewAndNavigation(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(120, 40)

	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
			{
				Role:    "assistant",
				Content: "hi there",
				ToolCalls: []types.MessageToolCall{
					{Name: "list_devices", Args: []byte(`{"customer_id":"acme"}`)},
				},
				ToolResult: &types.MessageToolResult{
					Name:    "list_devices",
					Content: `{"devices":[1,2]}`,
				},
			},
		},
	}

	panel.SetData("run-1", "scn", "prov", res)
	down := tea.KeyMsg{Type: tea.KeyDown}
	_ = panel.Update(down)
	out := panel.View()
	assert.Contains(t, out, "Conversation")
	assert.Contains(t, out, "list_devices")
	assert.Contains(t, out, "customer_id")
	assert.Contains(t, out, "Turn 2")
	assert.Contains(t, out, "Tokens:")
	assert.Contains(t, out, "scroll")
}

func TestConversationPanel_Reset(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(80, 30)
	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}
	panel.SetData("run-1", "", "", res)
	assert.NotEmpty(t, panel.View())

	panel.Reset()
	panel.SetData("run-1", "", "", nil)
	out := panel.View()
	assert.Contains(t, out, "No conversation")
}

func TestRunsPanel_Basic(t *testing.T) {
	panel := NewRunsPanel()
	panel.Init(30)

	runs := []RunInfo{
		{RunID: "run-1", Scenario: "s1", Provider: "p1", Status: StatusRunning},
		{RunID: "run-2", Scenario: "s2", Provider: "p2", Status: StatusCompleted},
	}

	panel.Update(runs, 100, 30)
	view := panel.View(true)
	assert.Contains(t, view, "s1")
	assert.Contains(t, view, "p1")
}

func TestLogsPanel_Basic(t *testing.T) {
	panel := NewLogsPanel()

	logs := []LogEntry{
		{Level: "INFO", Message: "test log"},
		{Level: "ERROR", Message: "error log"},
	}

	panel.Update(logs, 100, 30)
	view := panel.View(true)
	assert.NotEmpty(t, view)
}

func TestResultPanel_Basic(t *testing.T) {
	panel := NewResultPanel()
	panel.Update(100, 30)

	// Test empty
	view := panel.View(nil)
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "No run selected")
}

func TestSummaryPanel_Basic(t *testing.T) {
	panel := NewSummaryPanel()
	panel.Update(100)

	// Basic smoke test - full test would need viewmodel
	assert.NotNil(t, panel)
}
