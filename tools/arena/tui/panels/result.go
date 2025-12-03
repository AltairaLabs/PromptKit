package panels

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

const (
	resultPadding = 2
)

// ResultPanel manages the result display
type ResultPanel struct {
	width  int
	height int
}

// NewResultPanel creates a new result panel
func NewResultPanel() *ResultPanel {
	return &ResultPanel{}
}

// Update updates the panel dimensions
func (p *ResultPanel) Update(width, height int) {
	p.width = width
	p.height = height
}

// View renders the result panel
func (p *ResultPanel) View(res *statestore.RunResult, status views.RunStatus, focused bool) string {
	borderColor := theme.BorderColorUnfocused()
	if focused {
		borderColor = lipgloss.Color(theme.ColorWhite)
	}

	if res == nil {
		content := "No run selected"
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, resultPadding).
			Render(content)
	}

	resultView := views.NewResultView()
	content := resultView.Render(res, status)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, resultPadding).
		Render(content)
}
