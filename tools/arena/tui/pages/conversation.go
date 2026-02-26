// Package pages provides top-level page components for the TUI.
package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// ConversationPage renders the conversation view
type ConversationPage struct {
	panel *panels.ConversationPanel
}

// NewConversationPage creates a new conversation page
func NewConversationPage() *ConversationPage {
	return &ConversationPage{
		panel: panels.NewConversationPanel(),
	}
}

// Reset clears the conversation state
func (p *ConversationPage) Reset() {
	p.panel.Reset()
}

// SetDimensions updates the panel dimensions
func (p *ConversationPage) SetDimensions(width, height int) {
	p.panel.SetDimensions(width, height)
}

// SetData updates the conversation with run data
func (p *ConversationPage) SetData(runID, scenario, provider string, res *statestore.RunResult) {
	p.panel.SetData(runID, scenario, provider, res)
}

// Update handles input for the conversation panel
func (p *ConversationPage) Update(msg tea.Msg) tea.Cmd {
	return p.panel.Update(msg)
}

// Render renders the conversation page
func (p *ConversationPage) Render() string {
	return p.panel.View()
}

// GetKeyBindings returns the key bindings for this page
func (p *ConversationPage) GetKeyBindings() []views.KeyBinding {
	return []views.KeyBinding{
		{Keys: "↑/↓", Description: "navigate"},
		{Keys: "←/→ h/l", Description: "switch pane"},
		{Keys: "esc", Description: "back"},
	}
}

// Panel returns the underlying conversation panel for direct access
func (p *ConversationPage) Panel() *panels.ConversationPanel {
	return p.panel
}
