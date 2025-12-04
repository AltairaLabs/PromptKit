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
	// Minimum heights for proper display
	const minBottomHeight = 10
	const runsPanelChrome = 5 // border (2) + padding (2) + title (1)
	const minRunsTableHeight = 3

	// Calculate desired runs table height (height/3) but ensure bottom panels get their minimum first
	desiredRunsTableHeight := p.height / 3
	maxRunsActualHeight := p.height - minBottomHeight
	maxRunsTableHeight := maxRunsActualHeight - runsPanelChrome

	// Use the smaller of desired or max available, but at least the minimum
	runsTableHeight := desiredRunsTableHeight
	if runsTableHeight > maxRunsTableHeight {
		runsTableHeight = maxRunsTableHeight
	}
	if runsTableHeight < minRunsTableHeight {
		runsTableHeight = minRunsTableHeight
	}

	runsActualHeight := runsTableHeight + runsPanelChrome
	bottomHeight := p.height - runsActualHeight
	if bottomHeight < minBottomHeight {
		bottomHeight = minBottomHeight
	}

	// Update runs panel with calculated height (not full height)
	p.runsPanel.Update(p.runs, p.width, runsActualHeight)

	// Calculate 50/50 split for logs and result
	logsWidth := p.width / 2
	resultWidth := p.width - logsWidth

	// Update bottom panels with split dimensions and allocated height
	p.logsPanel.Update(p.logs, logsWidth, bottomHeight)
	p.resultPanel.Update(resultWidth, bottomHeight)

	// Build bottom row with logs (50%) and result (50%) side by side
	bottomRow := lipgloss.JoinHorizontal(
		lipgloss.Top,
		p.logsPanel.View(p.focusedPanel == "logs"),
		p.resultPanel.View(p.result, p.focusedPanel == "result"),
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

// SetFocusedPanel updates which panel has focus
func (p *MainPage) SetFocusedPanel(panel string) {
	p.focusedPanel = panel
}
