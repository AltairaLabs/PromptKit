package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
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
	table    table.Model
	viewport viewport.Model
	ready    bool
	width    int
	height   int
}

// NewResultPanel creates a new result panel
//
//nolint:mnd // Default viewport dimensions
func NewResultPanel() *ResultPanel {
	return &ResultPanel{
		viewport: viewport.New(80, 20), // Default size, will be updated
	}
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
		// Hide the cursor/selector
		s.Cell = s.Cell.PaddingLeft(0)
		s.Selected = s.Cell
		t.SetStyles(s)

		p.table = t
		p.ready = true
	} else {
		// Calculate table height based on allocated height
		// Height passed in is the full panel height, need to account for:
		// border (2) + padding (2) + title (1) = 5 lines of chrome
		const resultPanelChrome = 5
		tableHeight := height - resultPanelChrome
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

		// Update viewport dimensions
		// Account for border (2), padding (4), and title line
		viewportWidth := width - 6
		viewportHeight := tableHeight + 1 // +1 for title
		p.viewport.Width = viewportWidth
		p.viewport.Height = viewportHeight
	}
}

// View renders the result panel
func (p *ResultPanel) View(data *ResultPanelData, focused bool) string {
	if !p.ready {
		return "Loading..."
	}

	rows, summaryText := p.buildTableContent(data)
	p.table.SetRows(rows)

	title := theme.TitleStyle.Render(summaryText)
	tableView := p.table.View()

	// Apply color formatting by replacing symbols in the rendered output
	// This avoids breaking table width calculations
	greenCheck := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Render("✓")
	redX := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("✗")

	// Replace plain symbols with styled versions
	// This works because the table has already calculated widths
	tableView = strings.ReplaceAll(tableView, " ✓ ", " "+greenCheck+" ")
	tableView = strings.ReplaceAll(tableView, " ✗ ", " "+redX+" ")

	content := lipgloss.JoinVertical(lipgloss.Left, title, tableView)

	// Set viewport content and render
	p.viewport.SetContent(content)

	borderColor := theme.BorderColorUnfocused()
	if focused {
		borderColor = theme.BorderColorFocused()
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, resultPadding).
		Render(p.viewport.View())
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

	summaryText = fmt.Sprintf("Assertions (%d/%d passed)",
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
	// Use plain text symbols - styling in table cells breaks width calculations
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

// Viewport returns the underlying viewport for scrolling
func (p *ResultPanel) Viewport() *viewport.Model {
	return &p.viewport
}

// SetFocus sets the focus state of the panel
func (p *ResultPanel) SetFocus(focused bool) {
	if focused {
		p.table.Focus()
	} else {
		p.table.Blur()
	}
}
