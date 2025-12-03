package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

const (
	defaultRunsTableHeight     = 15
	runsTableHeightDivisor     = 3
	runsTableMinHeight         = 5
	runsTableWidthPadding      = 8
	runsPanelPadding           = 2
	runsPanelHorizontalPadding = runsPanelPadding * 2
	statusColWidth             = 10
	providerColWidth           = 20
	scenarioColWidth           = 30
	regionColWidth             = 12
	durationColWidth           = 12
	costColWidth               = 10
	notesColWidth              = 24
	errorNoteMaxLen            = 40
)

func (m *Model) renderActiveRuns() string {
	// Initialize table on first render
	if !m.tableReady {
		m.initRunsTable(defaultRunsTableHeight)
	}

	// Update table rows with current active runs
	m.updateRunsTable()

	// Set table dimensions
	tableHeight := m.height / runsTableHeightDivisor
	if tableHeight < runsTableMinHeight {
		tableHeight = runsTableMinHeight
	}
	m.runsTable.SetHeight(tableHeight)
	m.runsTable.SetWidth(m.width - runsTableWidthPadding)
	if m.activePane == paneRuns {
		m.runsTable.Focus()
	} else {
		m.runsTable.Blur()
	}

	borderColor := theme.BorderColorUnfocused()
	if m.activePane == paneRuns {
		borderColor = lipgloss.Color(theme.ColorWhite)
	}

	titleStyle := theme.TitleStyle

	title := titleStyle.Render(fmt.Sprintf("📊 Active Runs (%d concurrent workers)", len(m.activeRuns)))

	content := lipgloss.JoinVertical(lipgloss.Left, title, m.runsTable.View())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, runsPanelPadding).
		Width(m.width - runsPanelHorizontalPadding).
		Render(content)
}

// initRunsTable initializes the table for active runs
func (m *Model) initRunsTable(height int) {
	columns := []table.Column{
		{Title: "Status", Width: statusColWidth},
		{Title: "Provider", Width: providerColWidth},
		{Title: "Scenario", Width: scenarioColWidth},
		{Title: "Region", Width: regionColWidth},
		{Title: "Duration", Width: durationColWidth},
		{Title: "Cost", Width: costColWidth},
		{Title: "Notes", Width: notesColWidth},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(false),
		table.WithHeight(height),
	)

	// Style the table
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.BorderColorFocused()).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color(theme.ColorViolet))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(theme.ColorWhite)).
		Background(theme.BorderColorFocused()).
		Bold(false)

	t.SetStyles(s)
	m.runsTable = t
	m.tableReady = true
}

// updateRunsTable updates the table rows with current active runs
func (m *Model) updateRunsTable() {
	rows := make([]table.Row, 0, len(m.activeRuns))

	for i := range m.activeRuns {
		run := &m.activeRuns[i]

		var status, duration, cost, notes string
		switch run.Status {
		case StatusRunning:
			status = "● Running"
			elapsed := time.Since(run.StartTime)
			duration = theme.FormatDuration(elapsed)
			cost = "-"
			if run.CurrentTurnRole != "" {
				notes = fmt.Sprintf("turn %d: %s", run.CurrentTurnIndex+1, run.CurrentTurnRole)
			}
		case StatusCompleted:
			status = "✓ Done"
			duration = theme.FormatDuration(run.Duration)
			cost = theme.FormatCost(run.Cost)
		case StatusFailed:
			status = "✗ Failed"
			duration = "-"
			cost = "-"
			notes = theme.TruncateString(run.Error, errorNoteMaxLen)
		}

		if run.Selected {
			status = fmt.Sprintf("%s *", status)
		}

		rows = append(rows, table.Row{
			status,
			run.Provider,
			run.Scenario,
			run.Region,
			duration,
			cost,
			notes,
		})
	}

	m.runsTable.SetRows(rows)
}
