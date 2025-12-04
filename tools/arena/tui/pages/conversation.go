// Package pages provides top-level page components for the TUI.
package pages

import (
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	tea "github.com/charmbracelet/bubbletea"
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
