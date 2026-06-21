package pages

import (
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/layout"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// Pane IDs for the main page layout tree; these match the focusedPanel values
// set by the model.
const (
	paneIDRuns   = "runs"
	paneIDLogs   = "logs"
	paneIDResult = "result"
)

// Main page layout: runs on top (weight 1), logs and result split 50/50 below
// (weight 2). Minimums are box heights (panel chrome included).
const (
	minRunsBox       = 8  // runs table minimum (3) + chrome (5)
	minBottomBox     = 10 // logs/result minimum box height
	mainRunsWeight   = 1
	mainBottomWeight = 2
)

// mainLayoutTree builds the main page's layout tree. A fresh tree is created per
// page so its weights/visibility can be mutated independently (resize/collapse).
func mainLayoutTree() *layout.Node {
	return layout.VSplit(
		layout.Flex(mainRunsWeight, layout.Pane(paneIDRuns, layout.Min(0, minRunsBox))),
		layout.Flex(mainBottomWeight, layout.HSplit(
			layout.Pane(paneIDLogs, layout.Min(0, minBottomBox)),
			layout.Pane(paneIDResult, layout.Min(0, minBottomBox)),
		)),
	)
}

// MainPage renders the primary view with active runs and logs.
type MainPage struct {
	runsPanel   *panels.RunsPanel
	logsPanel   *panels.LogsPanel
	resultPanel *panels.ResultPanel

	layout *layout.Manager

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
		layout:      layout.NewManager(mainLayoutTree()),
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

// Render builds the main page body using the layout engine. Only visible panes
// (not collapsed) are sized and composed; collapsed panes drop out and their
// neighbors expand.
func (p *MainPage) Render() string {
	p.layout.SetArea(layout.Rect{W: p.width, H: p.height})

	if r, ok := p.layout.Rect(paneIDRuns); ok {
		p.runsPanel.Update(p.runs, r.W, r.H)
	}
	if r, ok := p.layout.Rect(paneIDLogs); ok {
		p.logsPanel.Update(p.logs, r.W, r.H)
	}
	if r, ok := p.layout.Rect(paneIDResult); ok {
		p.resultPanel.Update(r.W, r.H)
	}

	content := map[string]string{
		paneIDRuns:   p.runsPanel.View(p.focusedPanel == paneIDRuns),
		paneIDLogs:   p.logsPanel.View(p.focusedPanel == paneIDLogs),
		paneIDResult: p.resultPanel.View(p.result, p.focusedPanel == paneIDResult),
	}
	return layout.RenderTree(p.layout.Root(), content)
}

// GrowFocused resizes the currently focused pane by delta weight steps.
func (p *MainPage) GrowFocused(delta int) {
	if p.focusedPanel != "" {
		p.layout.Grow(p.focusedPanel, delta)
	}
}

// ToggleCollapseFocused collapses or restores the currently focused pane.
func (p *MainPage) ToggleCollapseFocused() {
	if p.focusedPanel != "" {
		p.layout.ToggleCollapse(p.focusedPanel)
	}
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
