// Package views provides pure rendering components for TUI views.
package views

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/viewmodels"
)

const (
	statusColWidth   = 10
	providerColWidth = 20
	scenarioColWidth = 30
	regionColWidth   = 12
	durationColWidth = 12
	costColWidth     = 10
	notesColWidth    = 24
)

// RunsTableView renders the active runs table
type RunsTableView struct {
	width      int
	height     int
	focused    bool
	tableStyle table.Styles
}

// NewRunsTableView creates a new RunsTableView
func NewRunsTableView() *RunsTableView {
	style := table.DefaultStyles()
	style.Header = style.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.BorderColorFocused()).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color(theme.ColorViolet))
	style.Selected = style.Selected.
		Foreground(lipgloss.Color(theme.ColorWhite)).
		Background(theme.BorderColorFocused()).
		Bold(false)

	return &RunsTableView{
		tableStyle: style,
	}
}

// SetDimensions updates the view dimensions
func (v *RunsTableView) SetDimensions(width, height int) {
	v.width = width
	v.height = height
}

// SetFocused sets the focused state
func (v *RunsTableView) SetFocused(focused bool) {
	v.focused = focused
}

// GetColumns returns the table columns
func (v *RunsTableView) GetColumns() []table.Column {
	return []table.Column{
		{Title: "Status", Width: statusColWidth},
		{Title: "Provider", Width: providerColWidth},
		{Title: "Scenario", Width: scenarioColWidth},
		{Title: "Region", Width: regionColWidth},
		{Title: "Duration", Width: durationColWidth},
		{Title: "Cost", Width: costColWidth},
		{Title: "Notes", Width: notesColWidth},
	}
}

// GetTableStyle returns the table style
func (v *RunsTableView) GetTableStyle() table.Styles {
	return v.tableStyle
}

// Render renders the runs table with the given view model
func (v *RunsTableView) Render(vm *viewmodels.RunsTableViewModel) string {
	columns := v.GetColumns()
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(vm.GetRows()),
		table.WithFocused(v.focused),
		table.WithHeight(v.height),
	)
	t.SetStyles(v.tableStyle)
	t.SetWidth(v.width)

	borderColor := theme.BorderColorUnfocused()
	if v.focused {
		borderColor = lipgloss.Color(theme.ColorWhite)
	}

	title := theme.TitleStyle.Render(
		fmt.Sprintf("📊 Active Runs (%d concurrent workers)", vm.GetRowCount()),
	)

	content := lipgloss.JoinVertical(lipgloss.Left, title, t.View())

	const (
		padding           = 2
		horizontalPadding = padding * 2
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, padding).
		Width(v.width - horizontalPadding).
		Render(content)
}
