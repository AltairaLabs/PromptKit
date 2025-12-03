package panels

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

	statusColWidth   = 10
	providerColWidth = 20
	scenarioColWidth = 30
	regionColWidth   = 12
	durationColWidth = 12
	costColWidth     = 10
	notesColWidth    = 24
	errorNoteMaxLen  = 40
)

// RunStatus represents the status of a run
type RunStatus int

// Run status constants
const (
	StatusRunning RunStatus = iota
	StatusCompleted
	StatusFailed
)

// RunInfo contains information about a single run
type RunInfo struct {
	RunID            string
	Scenario         string
	Provider         string
	Region           string
	Status           RunStatus
	Duration         time.Duration
	Cost             float64
	Error            string
	StartTime        time.Time
	CurrentTurnIndex int
	CurrentTurnRole  string
	Selected         bool
}

// RunsPanel manages the active runs table display
type RunsPanel struct {
	table table.Model
	ready bool
}

// NewRunsPanel creates a new runs panel
func NewRunsPanel() *RunsPanel {
	return &RunsPanel{}
}

// Init initializes the runs table
func (p *RunsPanel) Init(height int) {
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
		table.WithFocused(true),
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

	p.table = t
	p.ready = true
}

// Update updates the table with run data and dimensions
func (p *RunsPanel) Update(runs []RunInfo, width, height int) {
	if !p.ready {
		p.Init(defaultRunsTableHeight)
	}

	// Update table rows
	rows := make([]table.Row, 0, len(runs))
	for i := range runs {
		run := &runs[i]
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
	p.table.SetRows(rows)

	// Update dimensions
	tableHeight := height / runsTableHeightDivisor
	if tableHeight < runsTableMinHeight {
		tableHeight = runsTableMinHeight
	}
	p.table.SetHeight(tableHeight)
	p.table.SetWidth(width - runsTableWidthPadding)

	// Update focus
	// Note: focus is now managed externally via View's focused parameter
}

// View renders the runs panel
func (p *RunsPanel) View(focused bool) string {
	borderColor := theme.BorderColorUnfocused()
	if focused {
		borderColor = lipgloss.Color(theme.ColorWhite)
	}

	titleStyle := theme.TitleStyle
	title := titleStyle.Render("📊 Active Runs")

	content := lipgloss.JoinVertical(lipgloss.Left, title, p.table.View())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, runsPanelPadding).
		Render(content)
}

// Table returns the underlying table for key handling
func (p *RunsPanel) Table() *table.Model {
	return &p.table
}

// SetFocus sets the focus state of the table
func (p *RunsPanel) SetFocus(focused bool) {
	if focused {
		p.table.Focus()
	} else {
		p.table.Blur()
	}
}
