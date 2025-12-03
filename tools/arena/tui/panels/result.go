package panels

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

const (
	resultPadding = 2

	// Type column constraints
	maxTypeLen      = 25
	typeEllipsisLen = 22

	// Details column constraints
	maxDetailsLen      = 45
	detailsEllipsisLen = 42
)

// ResultPanelData contains the data needed to render the result panel
type ResultPanelData struct {
	Result *statestore.RunResult
	Status views.RunStatus
}

// ResultPanel manages the result display
type ResultPanel struct {
	table  table.Model
	ready  bool
	width  int
	height int
}

// NewResultPanel creates a new result panel
func NewResultPanel() *ResultPanel {
	return &ResultPanel{}
}

// Update updates the panel dimensions
//
//nolint:mnd // Table layout constants
func (p *ResultPanel) Update(width, height int) {
	p.width = width
	p.height = height

	if !p.ready {
		// Initialize table
		columns := []table.Column{
			{Title: "✓", Width: 2},
			{Title: "Type", Width: 20},
			{Title: "Details", Width: 30},
		}

		t := table.New(
			table.WithColumns(columns),
			table.WithRows([]table.Row{}),
			table.WithFocused(false),
			table.WithHeight(height/3),
		)

		// Style the table
		s := table.DefaultStyles()
		s.Header = s.Header.
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(theme.BorderColorUnfocused()).
			BorderBottom(true).
			Bold(true).
			Foreground(lipgloss.Color(theme.ColorViolet))
		s.Selected = s.Selected.
			Foreground(lipgloss.Color(theme.ColorWhite)).
			Background(theme.BorderColorUnfocused()).
			Bold(false)
		t.SetStyles(s)

		p.table = t
		p.ready = true
	} else {
		// Update table dimensions
		tableHeight := height / 3
		if tableHeight < 5 {
			tableHeight = 5
		}
		p.table.SetHeight(tableHeight)

		// Adjust column widths based on available width
		chromeWidth := 10 // borders, padding, etc
		availWidth := width - chromeWidth
		if availWidth < 30 {
			availWidth = 30
		}

		// Allocate width: Status (3) | Type (30%) | Details (70%)
		typeWidth := int(float64(availWidth) * 0.3)
		detailWidth := availWidth - typeWidth - 3

		p.table.SetColumns([]table.Column{
			{Title: "✓", Width: 2},
			{Title: "Type", Width: typeWidth},
			{Title: "Details", Width: detailWidth},
		})
	}
}

// View renders the result panel
func (p *ResultPanel) View(data *ResultPanelData) string {
	if !p.ready {
		return "Loading..."
	}

	rows, summaryText := p.buildTableContent(data)
	p.table.SetRows(rows)

	title := theme.TitleStyle.Render(summaryText)
	content := lipgloss.JoinVertical(lipgloss.Left, title, p.table.View())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderColorUnfocused()).
		Padding(1, resultPadding).
		Render(content)
}

// buildTableContent creates table rows and summary text from result data
func (p *ResultPanel) buildTableContent(data *ResultPanelData) (rows []table.Row, summaryText string) {
	if data == nil || data.Result == nil {
		return []table.Row{}, "No run selected"
	}

	res := data.Result
	if res.ConversationAssertions.Total == 0 {
		return []table.Row{}, "No assertions"
	}

	summaryText = fmt.Sprintf("📋 Assertions (%d/%d passed)",
		res.ConversationAssertions.Total-res.ConversationAssertions.Failed,
		res.ConversationAssertions.Total,
	)

	for _, result := range res.ConversationAssertions.Results {
		rows = append(rows, p.buildAssertionRow(result))
	}

	return rows, summaryText
}

// buildAssertionRow creates a table row for a single assertion result
func (p *ResultPanel) buildAssertionRow(result statestore.ConversationValidationResult) table.Row {
	status := "✓"
	if !result.Passed {
		status = "✗"
	}

	typeName := truncateString(result.Type, maxTypeLen, typeEllipsisLen)
	details := buildDetailsText(result)

	return table.Row{status, typeName, details}
}

// buildDetailsText creates the details column text from message and violations
func buildDetailsText(result statestore.ConversationValidationResult) string {
	details := result.Message
	if len(result.Violations) > 0 {
		if details != "" {
			details += " | "
		}
		details += fmt.Sprintf("%d violation(s)", len(result.Violations))
	}
	return truncateString(details, maxDetailsLen, detailsEllipsisLen)
}

// truncateString truncates a string to maxLen, adding ellipsis if needed
func truncateString(s string, maxLen, ellipsisLen int) string {
	if len(s) > maxLen {
		return s[:ellipsisLen] + "..."
	}
	return s
}

// Table returns the underlying table for key handling
func (p *ResultPanel) Table() *table.Model {
	return &p.table
}
