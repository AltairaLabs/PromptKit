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

// Render builds the main page body using the layout engine: runs on top
// (weight 1), logs and result split 50/50 below (weight 2). Each panel still
// renders its own chrome and sizes its internal viewport/table from the box.
func (p *MainPage) Render() string {
	const (
		minRunsBox   = 8  // runs table minimum (3) + chrome (5)
		minBottomBox = 10 // logs/result minimum box height
		runsWeight   = 1
		bottomWeight = 2
	)

	tree := layout.VSplit(
		layout.Flex(runsWeight, layout.Pane(paneIDRuns, layout.Min(0, minRunsBox))),
		layout.Flex(bottomWeight, layout.HSplit(
			layout.Pane(paneIDLogs, layout.Min(0, minBottomBox)),
			layout.Pane(paneIDResult, layout.Min(0, minBottomBox)),
		)),
	)
	rects := layout.Solve(tree, layout.Rect{W: p.width, H: p.height})

	p.runsPanel.Update(p.runs, rects[paneIDRuns].W, rects[paneIDRuns].H)
	p.logsPanel.Update(p.logs, rects[paneIDLogs].W, rects[paneIDLogs].H)
	p.resultPanel.Update(rects[paneIDResult].W, rects[paneIDResult].H)

	content := map[string]string{
		paneIDRuns:   p.runsPanel.View(p.focusedPanel == paneIDRuns),
		paneIDLogs:   p.logsPanel.View(p.focusedPanel == paneIDLogs),
		paneIDResult: p.resultPanel.View(p.result, p.focusedPanel == paneIDResult),
	}
	return layout.RenderTree(tree, content)
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
