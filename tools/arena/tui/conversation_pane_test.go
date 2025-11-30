package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestConversationPane_ViewAndNavigation(t *testing.T) {
	pane := NewConversationPane()
	pane.SetDimensions(120, 40)

	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
			{
				Role: "assistant", Content: "hi there",
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

	pane.SetData("run-1", res)
	down := tea.KeyMsg{Type: tea.KeyDown}
	newPane, _ := pane.Update(down)
	out := newPane.View(res)
	assert.Contains(t, out, "Conversation")
	assert.Contains(t, out, "list_devices")
	assert.Contains(t, out, "customer_id")
	assert.Contains(t, out, "Turn 2")
	assert.Contains(t, out, "Tokens:")
	assert.Contains(t, out, "scroll")
}

func TestConversationPane_Reset(t *testing.T) {
	pane := NewConversationPane()
	pane.SetDimensions(80, 30)
	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}
	pane.SetData("run-1", res)
	assert.NotEmpty(t, pane.View(res))

	pane.Reset()
	pane.SetData("run-1", nil)
	out := pane.View(&statestore.RunResult{Messages: []types.Message{}})
	assert.Contains(t, out, "No conversation")
}
