package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/reader/filesystem"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// ---------------------------------------------------------------------------
// Key constants (satisfies goconst: repeated string literals become constants)
// ---------------------------------------------------------------------------

const (
	keyTab   = "tab"
	keyEnter = "enter"

	// Page title constants (used in Title() and ChromeConfig.ConfigFile).
	titleView         = "View"
	titleResults      = "Results"
	titleConversation = "Conversation"
)

// ---------------------------------------------------------------------------
// Private message types (view-package-internal navigation signals)
// ---------------------------------------------------------------------------

// resultLoadedViewMsg is delivered when a single run result file has been read.
type resultLoadedViewMsg struct {
	runID      string
	scenarioID string
	providerID string
	result     *statestore.RunResult
}

// summaryLoadedViewMsg is delivered when an index.json summary has been read.
type summaryLoadedViewMsg struct {
	summary    map[string]interface{}
	resultsDir string
}

// resultsLoadedViewMsg is delivered when all run results have been loaded from
// a summary.
type resultsLoadedViewMsg struct {
	runs []*statestore.RunResult
}

// resultLoadErrorViewMsg is delivered when any load operation fails.
type resultLoadErrorViewMsg struct {
	err error
}

// ---------------------------------------------------------------------------
// ViewPage — file browser entry point
// ---------------------------------------------------------------------------

// ViewPage wraps a file browser to let the user navigate past Arena result
// directories. Selecting a file triggers an async load; the result msg is then
// used to push either a ResultsPage (index.json) or ConversationViewPage.
type ViewPage struct {
	browser    *pages.FileBrowserPage
	resultsDir string
	err        error
	w, h       int
}

// NewViewPage creates a ViewPage rooted at resultsDir.
func NewViewPage(resultsDir string) *ViewPage {
	return &ViewPage{
		browser:    pages.NewFileBrowserPage(resultsDir),
		resultsDir: resultsDir,
	}
}

// Init implements Page. Delegates to the underlying file browser.
func (p *ViewPage) Init() tea.Cmd {
	return p.browser.Init()
}

// Update implements Page.
//
//   - pages.FileSelectedMsg — start async load (summary or individual result)
//   - summaryLoadedViewMsg — push ResultsPage
//   - resultLoadedViewMsg — push ConversationViewPage
//   - resultLoadErrorViewMsg — store error for View
//   - all other messages — forward to browser
func (p *ViewPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch m := msg.(type) {
	case pages.FileSelectedMsg:
		p.err = nil
		if filepath.Base(m.Path) == "index.json" {
			// m.RunID is intentionally unused for index/summary files; the run
			// IDs are extracted from the file contents instead.
			return p, loadSummaryFile(m.Path, p.resultsDir)
		}
		return p, loadRunResult(m.RunID, p.resultsDir)

	case summaryLoadedViewMsg:
		rp := NewResultsPage(m.summary, m.resultsDir)
		return p, func() tea.Msg { return PushPageMsg{Page: rp} }

	case resultLoadedViewMsg:
		cvp := NewConversationViewPage(m.runID, m.scenarioID, m.providerID, m.result)
		return p, func() tea.Msg { return PushPageMsg{Page: cvp} }

	case resultLoadErrorViewMsg:
		p.err = m.err
		return p, nil
	}

	_, cmd := p.browser.Update(msg)
	return p, cmd
}

// View implements Page.
func (p *ViewPage) View() string {
	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:  p.w,
			Height: p.h,
			Title:  "View results",
			KeyBindings: []views.KeyBinding{
				{Keys: "↑/↓", Description: "navigate"},
				{Keys: "enter", Description: "open"},
				{Keys: "←/h", Description: "parent dir"},
				{Keys: "esc", Description: "back"},
			},
		},
		func(contentHeight int) string {
			p.browser.SetDimensions(p.w, contentHeight)
			if p.err != nil {
				errorStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(theme.ColorError)).
					Bold(true)
				helpStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(theme.ColorGray)).
					Italic(true)
				return lipgloss.JoinVertical(lipgloss.Left,
					errorStyle.Render("error: "+p.err.Error()),
					helpStyle.Render("select a different file, or press Esc to go back"),
					"",
					p.browser.Render(),
				)
			}
			return p.browser.Render()
		},
	)
}

// Title implements Page.
func (p *ViewPage) Title() string { return titleView }

// SetSize implements Page. Stores dimensions for use in View().
func (p *ViewPage) SetSize(w, h int) {
	p.w, p.h = w, h
}

// ---------------------------------------------------------------------------
// ResultsPage — summary / index view
// ---------------------------------------------------------------------------

// Panel focus constants for the results page.
const (
	resultsPageFocusRuns   = "runs"
	resultsPageFocusLogs   = "logs"
	resultsPageFocusResult = "result"
)

// ResultsPage wraps MainPage to display the results loaded from a summary
// (index.json). The user can navigate the runs table and press Enter to dive
// into a ConversationViewPage.
type ResultsPage struct {
	mainPage      *pages.MainPage
	summary       map[string]interface{}
	resultsDir    string
	loadedResults []*statestore.RunResult
	mainPageFocus string
	lastError     error
	w, h          int
}

// NewResultsPage creates a ResultsPage for the given summary and directory.
func NewResultsPage(summary map[string]interface{}, resultsDir string) *ResultsPage {
	return &ResultsPage{
		mainPage:      pages.NewMainPage(),
		summary:       summary,
		resultsDir:    resultsDir,
		mainPageFocus: resultsPageFocusRuns,
	}
}

// Init implements Page. Triggers async loading of all run results listed in
// the summary.
func (p *ResultsPage) Init() tea.Cmd {
	return loadResultsFromSummary(p.summary, p.resultsDir)
}

// Update implements Page.
func (p *ResultsPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch m := msg.(type) {
	case resultsLoadedViewMsg:
		p.loadedResults = m.runs
		p.lastError = nil
		runs := convertResultsToViewData(m.runs)
		p.mainPage.SetData(runs, []panels.LogEntry{}, p.mainPageFocus, nil)
		return p, nil

	case resultLoadErrorViewMsg:
		p.lastError = m.err
		return p, nil

	case tea.KeyMsg:
		switch m.String() {
		case keyTab:
			nextFocus := map[string]string{
				resultsPageFocusRuns:   resultsPageFocusLogs,
				resultsPageFocusLogs:   resultsPageFocusResult,
				resultsPageFocusResult: resultsPageFocusRuns,
			}
			p.mainPageFocus = nextFocus[p.mainPageFocus]
			p.mainPage.SetFocusedPanel(p.mainPageFocus)
			return p, nil

		case keyEnter:
			return p.handleRunSelection()
		}
	}

	return p, nil
}

// handleRunSelection opens the currently selected run in a ConversationViewPage.
func (p *ResultsPage) handleRunSelection() (Page, tea.Cmd) {
	runsPanel := p.mainPage.RunsPanel()
	if runsPanel == nil {
		return p, nil
	}
	t := runsPanel.Table()
	if t == nil {
		return p, nil
	}
	idx := t.Cursor()
	if idx < 0 || idx >= len(p.loadedResults) {
		return p, nil
	}
	result := p.loadedResults[idx]
	cvp := NewConversationViewPage(result.RunID, result.ScenarioID, result.ProviderID, result)
	return p, func() tea.Msg { return PushPageMsg{Page: cvp} }
}

// View implements Page.
func (p *ResultsPage) View() string {
	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:  p.w,
			Height: p.h,
			Title:  titleResults,
			KeyBindings: []views.KeyBinding{
				{Keys: "↑/↓", Description: "navigate"},
				{Keys: "tab", Description: "cycle focus"},
				{Keys: "enter", Description: "open conversation"},
				{Keys: "esc", Description: "back"},
			},
		},
		func(contentHeight int) string {
			p.mainPage.SetDimensions(p.w, contentHeight)
			return p.mainPage.Render()
		},
	)
}

// Title implements Page.
func (p *ResultsPage) Title() string { return titleResults }

// SetSize implements Page.
func (p *ResultsPage) SetSize(w, h int) {
	p.w, p.h = w, h
}

// ---------------------------------------------------------------------------
// ConversationViewPage — individual run conversation view
// ---------------------------------------------------------------------------

// ConversationViewPage wraps pages.ConversationPage for use inside the hub
// shell navigation stack. When live, it streams runtime events into the panel
// (new turns, audio meter) until the run completes; otherwise it shows a static
// snapshot loaded from the state store.
type ConversationViewPage struct {
	convPage *pages.ConversationPage
	w, h     int

	live  bool
	runID string
	feed  *liveFeed

	logs *LogsOverlay
}

// NewConversationViewPage creates a ConversationViewPage pre-loaded with the
// given (completed-run) snapshot. It does not stream live events.
func NewConversationViewPage(runID, scenarioID, providerID string, result *statestore.RunResult) *ConversationViewPage {
	cp := pages.NewConversationPage()
	cp.SetData(runID, scenarioID, providerID, result)
	return &ConversationViewPage{convPage: cp, logs: NewLogsOverlay()}
}

// NewLiveConversationViewPage creates a ConversationViewPage that streams the
// conversation live while focused. seed may be a partial snapshot (turns so
// far) or nil; later runtime events are tailed via liveFeed and deduped against
// the seed. The page reverts to static once the run completes or fails.
func NewLiveConversationViewPage(
	runID, scenarioID, providerID string, seed *statestore.RunResult,
) *ConversationViewPage {
	cp := pages.NewConversationPage()
	// The panel only appends when it holds a non-nil result, so seed an empty
	// one when there is no snapshot yet.
	if seed == nil {
		seed = &statestore.RunResult{}
	}
	cp.SetData(runID, scenarioID, providerID, seed)
	return &ConversationViewPage{
		convPage: cp,
		live:     true,
		runID:    runID,
		feed:     newLiveFeed(runID, len(seed.Messages)),
		logs:     NewLogsOverlay(),
	}
}

// Init implements Page.
func (p *ConversationViewPage) Init() tea.Cmd { return nil }

// Update implements Page. While live, runtime events are applied to the panel
// via the feed; run completion flips the page back to static. Other messages
// (scroll/focus keys) forward to the underlying conversation panel.
func (p *ConversationViewPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	// Buffer runtime logs and toggle the logs overlay with 'L'. While the
	// overlay is visible it consumes scroll keys.
	if lm, ok := msg.(logging.Msg); ok {
		p.logs.Append(lm)
		return p, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "L" {
		p.logs.Toggle()
		return p, nil
	}
	if p.logs.Visible() {
		return p, p.logs.Update(msg)
	}

	if p.live {
		switch m := msg.(type) {
		case tui.RunCompletedMsg:
			if m.RunID == p.runID {
				p.live = false
			}
		case tui.RunFailedMsg:
			if m.RunID == p.runID {
				p.live = false
			}
		}
		if p.feed.Apply(p.convPage.Panel(), msg) {
			return p, nil
		}
	}
	cmd := p.convPage.Update(msg)
	return p, cmd
}

// View implements Page. When the logs overlay is visible it replaces the
// conversation body; otherwise the conversation renders as usual.
func (p *ConversationViewPage) View() string {
	bindings := p.convPage.GetKeyBindings()
	bindings = append(bindings, views.KeyBinding{Keys: "L", Description: resultsPageFocusLogs})
	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:       p.w,
			Height:      p.h,
			Title:       titleConversation,
			KeyBindings: bindings,
		},
		func(contentHeight int) string {
			if p.logs.Visible() {
				p.logs.SetSize(p.w, contentHeight)
				return p.logs.View()
			}
			p.convPage.SetDimensions(p.w, contentHeight)
			return p.convPage.Render()
		},
	)
}

// Title implements Page.
func (p *ConversationViewPage) Title() string { return titleConversation }

// SetSize implements Page.
func (p *ConversationViewPage) SetSize(w, h int) {
	p.w, p.h = w, h
	p.logs.SetSize(w, h)
}

// ---------------------------------------------------------------------------
// Async loading helpers (ported from cmd/promptarena/view.go)
// ---------------------------------------------------------------------------

// loadSummaryFile reads index.json and returns a summaryLoadedViewMsg.
func loadSummaryFile(path, resultsDir string) tea.Cmd {
	return func() tea.Msg {
		//nolint:gosec // Path comes from trusted file browser selection.
		data, err := os.ReadFile(path)
		if err != nil {
			return resultLoadErrorViewMsg{err: fmt.Errorf("failed to load summary: %w", err)}
		}

		var summary map[string]interface{}
		if err := json.Unmarshal(data, &summary); err != nil {
			return resultLoadErrorViewMsg{err: fmt.Errorf("failed to parse summary: %w", err)}
		}

		return summaryLoadedViewMsg{summary: summary, resultsDir: resultsDir}
	}
}

// loadRunResult loads a single run result from the filesystem.
func loadRunResult(runID, resultsDir string) tea.Cmd {
	return func() tea.Msg {
		reader := filesystem.NewFilesystemResultReader(resultsDir)
		result, err := reader.LoadResult(runID)
		if err != nil {
			return resultLoadErrorViewMsg{err: err}
		}
		return resultLoadedViewMsg{
			runID:      result.RunID,
			scenarioID: result.ScenarioID,
			providerID: result.ProviderID,
			result:     result,
		}
	}
}

// loadResultsFromSummary extracts run IDs from a summary map and loads each
// result, returning a resultsLoadedViewMsg.
func loadResultsFromSummary(summary map[string]interface{}, resultsDir string) tea.Cmd {
	return func() tea.Msg {
		runIDsInterface, ok := summary["run_ids"]
		if !ok {
			// No run_ids — return empty results rather than an error so the page
			// still renders correctly.
			return resultsLoadedViewMsg{runs: nil}
		}

		runIDsSlice, ok := runIDsInterface.([]interface{})
		if !ok {
			return resultLoadErrorViewMsg{err: fmt.Errorf("run_ids is not an array")}
		}

		runIDs := make([]string, 0, len(runIDsSlice))
		for _, id := range runIDsSlice {
			if strID, ok := id.(string); ok {
				runIDs = append(runIDs, strID)
			}
		}

		if len(runIDs) == 0 {
			return resultsLoadedViewMsg{runs: nil}
		}

		reader := filesystem.NewFilesystemResultReader(resultsDir)
		results, err := reader.LoadResults(runIDs)
		if err != nil {
			return resultLoadErrorViewMsg{err: fmt.Errorf("failed to load results: %w", err)}
		}

		return resultsLoadedViewMsg{runs: results}
	}
}

// convertResultsToViewData converts loaded RunResults into the RunInfo slice
// consumed by panels.RunsPanel.
func convertResultsToViewData(results []*statestore.RunResult) []panels.RunInfo {
	runs := make([]panels.RunInfo, len(results))
	for i, result := range results {
		status := panels.StatusCompleted
		if result.Error != "" {
			status = panels.StatusFailed
		}
		runs[i] = panels.RunInfo{
			RunID:    result.RunID,
			Scenario: result.ScenarioID,
			Provider: result.ProviderID,
			Region:   result.Region,
			Status:   status,
			Duration: result.Duration,
			Cost:     result.Cost.TotalCost,
			Error:    result.Error,
		}
	}
	return runs
}
