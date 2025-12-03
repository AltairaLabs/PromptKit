package pages

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// MainPage renders the primary view with active runs and logs.
type MainPage struct {
	runsPanel *panels.RunsPanel
	logsPanel *panels.LogsPanel

	// Stored data
	width        int
	height       int
	runs         []panels.RunInfo
	logs         []panels.LogEntry
	focusedPanel string
}

// NewMainPage creates a new main page with all panels
func NewMainPage() *MainPage {
	return &MainPage{
		runsPanel: panels.NewRunsPanel(),
		logsPanel: panels.NewLogsPanel(),
	}
}

// SetDimensions updates the page dimensions
func (p *MainPage) SetDimensions(width, height int) {
	p.width = width
	p.height = height
}

// SetData updates the page with run and log data
func (p *MainPage) SetData(runs []panels.RunInfo, logs []panels.LogEntry, focusedPanel string) {
	p.runs = runs
	p.logs = logs
	p.focusedPanel = focusedPanel
}

// Render builds the main page body
func (p *MainPage) Render() string {
	// Update panels with dimensions - let them use their own sizing logic
	p.runsPanel.Update(p.runs, p.width, p.height)
	p.logsPanel.Update(p.logs, p.width, p.height)

	// Simple vertical stack of runs and logs
	return lipgloss.JoinVertical(
		lipgloss.Left,
		p.runsPanel.View(p.focusedPanel == "runs"),
		p.logsPanel.View(p.focusedPanel == "logs"),
	)
}

// RunsPanel returns the runs panel for direct access (e.g., key handling)
func (p *MainPage) RunsPanel() *panels.RunsPanel {
	return p.runsPanel
}

// LogsPanel returns the logs panel for direct access (e.g., viewport scrolling)
func (p *MainPage) LogsPanel() *panels.LogsPanel {
	return p.logsPanel
}
