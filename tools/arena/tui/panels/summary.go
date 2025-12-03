package panels

import (
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/viewmodels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// SummaryPanel manages the summary display
type SummaryPanel struct {
	width int
}

// NewSummaryPanel creates a new summary panel
func NewSummaryPanel() *SummaryPanel {
	return &SummaryPanel{}
}

// Update updates the panel width
func (p *SummaryPanel) Update(width int) {
	p.width = width
}

// View renders the summary panel
func (p *SummaryPanel) View(summaryVM *viewmodels.SummaryViewModel, focused bool) string {
	summaryView := views.NewSummaryView(p.width, false)
	return summaryView.Render(summaryVM)
}
