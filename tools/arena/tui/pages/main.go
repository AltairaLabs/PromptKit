package pages

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// MainPage renders the primary view with active runs and logs.
type MainPage struct {
	runsPanel   *panels.RunsPanel
	logsPanel   *panels.LogsPanel
	resultPanel *panels.ResultPanel

	// Stored data
	width        int
	height       int
	runs         []panels.RunInfo
	logs         []panels.LogEntry
	focusedPanel string
	result       *panels.ResultPanelData
}

// NewMainPage creates a new main page with all panels
func NewMainPage() *MainPage {
	return &MainPage{
		runsPanel:   panels.NewRunsPanel(),
		logsPanel:   panels.NewLogsPanel(),
		resultPanel: panels.NewResultPanel(),
	}
}

// SetDimensions updates the page dimensions
func (p *MainPage) SetDimensions(width, height int) {
	p.width = width
	p.height = height
}

// SetData updates the page with run and log data
//
//nolint:lll // Parameter list
func (p *MainPage) SetData(runs []panels.RunInfo, logs []panels.LogEntry, focusedPanel string, result *panels.ResultPanelData) {
	p.runs = runs
	p.logs = logs
	p.focusedPanel = focusedPanel
	p.result = result
}

// Render builds the main page body
//
//nolint:mnd // Layout calculation constants
func (p *MainPage) Render() string {
	// The runs panel calculates its table height as height/3 and adds chrome
	// (title + border + padding). So actual runs panel height ≈ (height/3) + chrome
	// We need to give the bottom panels the remaining space
	runsTableHeight := p.height / 3
	runsPanelChrome := 0 // title + border + padding
	runsActualHeight := runsTableHeight + runsPanelChrome
	bottomHeight := (p.height - runsActualHeight) + 8 // Add 4 lines for bottom panels
	if bottomHeight < 10 {
		bottomHeight = 10
	}

	// Update runs panel with full width
	p.runsPanel.Update(p.runs, p.width, p.height)

	// Calculate 60/40 split for logs and result
	logsWidth := int(float64(p.width) * 0.6)
	resultWidth := p.width - logsWidth

	// Update bottom panels with split dimensions and allocated height
	p.logsPanel.Update(p.logs, logsWidth, bottomHeight)
	p.resultPanel.Update(resultWidth, bottomHeight)

	// Build bottom row with logs (60%) and result (40%) side by side
	bottomRow := lipgloss.JoinHorizontal(
		lipgloss.Top,
		p.logsPanel.View(p.focusedPanel == "logs"),
		p.resultPanel.View(p.result),
	)

	// Stack runs on top and logs/result on bottom
	return lipgloss.JoinVertical(
		lipgloss.Left,
		p.runsPanel.View(p.focusedPanel == "runs"),
		bottomRow,
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

// ResultPanel returns the result panel for direct access (e.g., viewport scrolling)
func (p *MainPage) ResultPanel() *panels.ResultPanel {
	return p.resultPanel
}
