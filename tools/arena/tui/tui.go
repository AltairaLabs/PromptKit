// Package tui provides a terminal user interface for PromptArena execution monitoring.
// It implements a multi-pane display showing active runs, metrics, and logs in real-time.
package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// Terminal size requirements
const (
	MinTerminalWidth  = 80
	MinTerminalHeight = 24
)

// Display constants
const (
	borderPadding       = 4
	maxLogBufferSize    = 100
	tickIntervalMs      = 500
	durationPrecisionMs = 100
	numberSeparator     = 1000
	panelDivisor        = 2
	bannerLines         = 2  // Banner + info line (separator counted separately)
	bottomPanelPadding  = 4  // Border + padding
	metricsBoxWidth     = 40 // Fixed width for metrics box
	summaryWidthDivisor = 2
	summaryMinWidth     = 40
)

// Color constants
const (
	colorPurple    = "#7C3AED"
	colorDarkBg    = "#1E1E2E"
	colorGreen     = "#10B981"
	colorBlue      = "#3B82F6"
	colorLightBlue = "#60A5FA"
	colorRed       = "#EF4444"
	colorAmber     = "#F59E0B"
	colorYellow    = "#FBBF24"
	colorGray      = "#6B7280"
	colorLightGray = "#9CA3AF"
	colorWhite     = "#F3F4F6"
	colorIndigo    = "#6366F1"
	colorViolet    = "#A78BFA"
	colorEmerald   = "#34D399"
	colorSky       = "#93C5FD"
)

type pane int

const (
	paneRuns pane = iota
	paneLogs
)

type page int

const (
	pageMain page = iota
	pageConversation
)

// runResultStore is a minimal interface to retrieve run results from the state store.
type runResultStore interface {
	GetResult(ctx context.Context, runID string) (*statestore.RunResult, error)
}

// Model represents the bubbletea application state
type Model struct {
	mu sync.Mutex

	width  int
	height int

	configFile     string
	totalRuns      int
	startTime      time.Time
	activeRuns     []RunInfo
	completedCount int
	successCount   int
	failedCount    int
	totalCost      float64
	totalTokens    int64
	totalDuration  time.Duration

	logs          []LogEntry
	logViewport   viewport.Model
	viewportReady bool
	runsTable     table.Model
	tableReady    bool

	isTUIMode      bool
	fallbackReason string

	// Summary state
	summary     *Summary
	showSummary bool

	stateStore runResultStore

	activePane pane

	// Conversation view state
	convPane ConversationPane

	currentPage page
}

// RunInfo tracks information about a single run
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

// RunStatus represents the current state of a run
type RunStatus int

const (
	// StatusRunning indicates the run is currently executing
	StatusRunning RunStatus = iota
	// StatusCompleted indicates the run completed successfully
	StatusCompleted
	// StatusFailed indicates the run failed with an error
	StatusFailed
)

// LogEntry represents a single log line
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

type tickMsg time.Time

// Init initializes the bubbletea model
func (m *Model) Init() tea.Cmd {
	return tick()
}

// Update processes bubbletea messages and updates the model state
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.mu.Lock()
		m.width = msg.Width
		m.height = msg.Height

		// Initialize viewport on first size update
		if !m.viewportReady {
			m.initViewport()
			m.viewportReady = true
		}
		// Initialize table on first size update
		if !m.tableReady {
			m.initRunsTable(10) // Start with reasonable default
		}
		m.mu.Unlock()
		return m, nil

	case tickMsg:
		return m, tick()

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		// Let viewport handle mouse wheel scrolling
		if m.viewportReady {
			var cmd tea.Cmd
			m.logViewport, cmd = m.logViewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case RunStartedMsg:
		m.mu.Lock()
		m.handleRunStarted(&msg)
		m.mu.Unlock()
		return m, nil

	case RunCompletedMsg:
		m.mu.Lock()
		m.handleRunCompleted(&msg)
		m.mu.Unlock()
		return m, nil

	case RunFailedMsg:
		m.mu.Lock()
		m.handleRunFailed(&msg)
		m.mu.Unlock()
		return m, nil

	case TurnStartedMsg:
		m.mu.Lock()
		m.handleTurnStarted(&msg)
		m.mu.Unlock()
		return m, nil

	case TurnCompletedMsg:
		m.mu.Lock()
		m.handleTurnCompleted(&msg)
		m.mu.Unlock()
		return m, nil

	case LogMsg:
		m.handleLogMsg(&msg)
		return m, nil

	case ShowSummaryMsg:
		m.handleShowSummary(&msg)
		// Don't quit immediately - let user see summary and press Ctrl+C or 'q' to exit
		return m, nil
	}

	return m, nil
}

// handleRunStarted processes a run started event from the observer.
// Note: Called with mutex already locked by Update.
func (m *Model) handleRunStarted(msg *RunStartedMsg) {
	// Add to active runs
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     msg.RunID,
		Scenario:  msg.Scenario,
		Provider:  msg.Provider,
		Region:    msg.Region,
		Status:    StatusRunning,
		StartTime: msg.Time,
	})

	// Log the event
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "INFO",
		Message:   fmt.Sprintf("Started: %s/%s/%s", msg.Provider, msg.Scenario, msg.Region),
	})
	m.trimLogs()
}

// handleRunCompleted processes a run completed event from the observer.
// Note: Called with mutex already locked by Update.
func (m *Model) handleRunCompleted(msg *RunCompletedMsg) {
	// Find and update the run in activeRuns
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID == msg.RunID {
			m.activeRuns[i].Status = StatusCompleted
			m.activeRuns[i].Duration = msg.Duration
			m.activeRuns[i].Cost = msg.Cost
			m.activeRuns[i].CurrentTurnRole = ""
			m.activeRuns[i].CurrentTurnIndex = 0
			break
		}
	}

	// Update metrics
	m.completedCount++
	m.successCount++
	m.totalDuration += msg.Duration
	m.totalCost += msg.Cost

	// Log the event
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "INFO",
		Message:   fmt.Sprintf("Completed: %s (%.1fs, $%.4f)", msg.RunID, msg.Duration.Seconds(), msg.Cost),
	})
	m.trimLogs()

	// Remove from active runs after a brief display (keep for visual feedback)
}

// handleRunFailed processes a run failed event from the observer.
// Note: Called with mutex already locked by Update.
func (m *Model) handleRunFailed(msg *RunFailedMsg) {
	// Find and update the run in activeRuns
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID == msg.RunID {
			m.activeRuns[i].Status = StatusFailed
			m.activeRuns[i].Error = msg.Error.Error()
			m.activeRuns[i].CurrentTurnRole = ""
			m.activeRuns[i].CurrentTurnIndex = 0
			break
		}
	}

	// Update metrics
	m.completedCount++
	m.failedCount++

	// Log the event
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "ERROR",
		Message:   fmt.Sprintf("Failed: %s - %v", msg.RunID, msg.Error),
	})
	m.trimLogs()
}

// handleTurnStarted updates the run with turn information.
func (m *Model) handleTurnStarted(msg *TurnStartedMsg) {
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID == msg.RunID {
			m.activeRuns[i].CurrentTurnRole = msg.Role
			m.activeRuns[i].CurrentTurnIndex = msg.TurnIndex
			break
		}
	}

	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "INFO",
		Message:   fmt.Sprintf("Turn %d (%s) started: %s", msg.TurnIndex+1, msg.Role, msg.RunID),
	})
	m.trimLogs()
}

// handleTurnCompleted updates the run when a turn finishes.
func (m *Model) handleTurnCompleted(msg *TurnCompletedMsg) {
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID == msg.RunID {
			m.activeRuns[i].CurrentTurnRole = msg.Role
			m.activeRuns[i].CurrentTurnIndex = msg.TurnIndex
			if msg.Error != nil {
				m.activeRuns[i].Error = msg.Error.Error()
			}
			break
		}
	}

	level := "INFO"
	text := fmt.Sprintf("Turn %d (%s) completed: %s", msg.TurnIndex+1, msg.Role, msg.RunID)
	if msg.Error != nil {
		level = "ERROR"
		text = fmt.Sprintf("Turn %d (%s) failed: %s - %v", msg.TurnIndex+1, msg.Role, msg.RunID, msg.Error)
	}

	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     level,
		Message:   text,
	})
	m.trimLogs()
}

// handleLogMsg processes a log message from the interceptor
func (m *Model) handleLogMsg(msg *LogMsg) {
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Timestamp,
		Level:     msg.Level,
		Message:   msg.Message,
	})
	m.trimLogs()

	// Auto-scroll viewport to bottom when new log arrives (only if viewport is ready)
	if m.viewportReady {
		m.logViewport.GotoBottom()
	}
}

// handleShowSummary processes a show summary message and switches to summary view
func (m *Model) handleShowSummary(msg *ShowSummaryMsg) {
	m.summary = msg.Summary
	m.showSummary = true
}

// trimLogs keeps the log buffer size within limits.
func (m *Model) trimLogs() {
	if len(m.logs) > maxLogBufferSize {
		m.logs = m.logs[len(m.logs)-maxLogBufferSize:]
	}
}

// View renders the TUI
func (m *Model) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isTUIMode {
		return ""
	}

	// Show summary if execution is complete
	if m.showSummary && m.summary != nil {
		return RenderSummary(m.summary, m.width)
	}

	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	elapsed := time.Since(m.startTime).Truncate(time.Second)

	header := m.renderHeader(elapsed)
	var body string
	switch m.currentPage {
	case pageConversation:
		body = m.renderConversationPage()
	case pageMain:
		body = lipgloss.JoinVertical(lipgloss.Left, m.renderActiveRuns(), "", m.renderLogs())
	default:
		body = lipgloss.JoinVertical(lipgloss.Left, m.renderActiveRuns(), "", m.renderLogs())
	}

	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}

func (m *Model) renderConversationPage() string {
	selected := m.selectedRun()
	if selected == nil {
		return "Select a run to view the conversation."
	}
	if m.stateStore == nil {
		return "No state store attached."
	}
	res, err := m.stateStore.GetResult(context.Background(), selected.RunID)
	if err != nil {
		return fmt.Sprintf("Failed to load result: %v", err)
	}
	m.convPane.SetDimensions(m.width, m.height)
	m.convPane.SetData(selected, res)
	return m.convPane.View(res)
}

func (m *Model) currentRunForDetail() *RunInfo {
	if sel := m.selectedRun(); sel != nil {
		return sel
	}
	if m.tableReady {
		idx := m.runsTable.Cursor()
		if idx >= 0 && idx < len(m.activeRuns) {
			return &m.activeRuns[idx]
		}
	}
	if len(m.activeRuns) > 0 {
		return &m.activeRuns[0]
	}
	return nil
}

func (m *Model) renderResultPane() string {
	run := m.currentRunForDetail()
	if run == nil {
		return ""
	}
	return m.renderSelectedResult(run)
}

func (m *Model) renderSummaryPane() string {
	summary := m.BuildSummary("", "")
	if summary == nil {
		return ""
	}
	// Allocate roughly half the width for summary.
	width := m.width / summaryWidthDivisor
	if width < summaryMinWidth {
		width = summaryMinWidth
	}
	return RenderSummary(summary, width)
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check for quit keys first
	//nolint:exhaustive // Only handling specific quit keys
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyRunes:
		if len(msg.Runes) > 0 && msg.Runes[0] == 'q' {
			return m, tea.Quit
		}
	default:
	}

	// Escape deselects a conversation and returns to main view
	if msg.Type == tea.KeyEsc && m.currentPage == pageConversation {
		m.deselectRuns()
		m.convPane.Reset()
		m.currentPage = pageMain
		m.activePane = paneRuns
		return m, nil
	}

	if m.currentPage == pageMain {
		return m.handleMainPageKey(msg)
	}

	// Conversation page key handling
	newPane, cmd := m.convPane.Update(msg)
	m.convPane = newPane
	return m, cmd
}

func (m *Model) handleMainPageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyTab {
		if m.activePane == paneRuns {
			m.activePane = paneLogs
		} else {
			m.activePane = paneRuns
		}
		return m, nil
	}

	if msg.Type == tea.KeyEnter && m.activePane == paneRuns && m.tableReady {
		m.toggleSelection()
		return m, nil
	}

	if m.activePane == paneRuns && m.tableReady {
		var cmd tea.Cmd
		m.runsTable, cmd = m.runsTable.Update(msg)
		return m, cmd
	}

	if m.viewportReady {
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) toggleSelection() {
	idx := m.runsTable.Cursor()
	if idx < 0 || idx >= len(m.activeRuns) {
		return
	}

	targetSelected := m.activeRuns[idx].Selected
	m.deselectRuns()
	m.activeRuns[idx].Selected = !targetSelected
	if m.activeRuns[idx].Selected {
		m.convPane.Reset()
		m.currentPage = pageConversation
	}
}

func (m *Model) deselectRuns() {
	for i := range m.activeRuns {
		m.activeRuns[i].Selected = false
	}
}

// BuildSummary creates a Summary from the current model state with output directory and HTML report path.
func (m *Model) BuildSummary(outputDir, htmlReport string) *Summary {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If a state store is available, try to reconstruct results for richer summary
	if m.stateStore != nil && len(m.activeRuns) > 0 {
		return m.buildSummaryFromStateStore(outputDir, htmlReport)
	}

	// Calculate average duration
	avgDuration := time.Duration(0)
	if m.completedCount > 0 {
		avgDuration = m.totalDuration / time.Duration(m.completedCount)
	}

	// Collect provider counts
	providerCounts := make(map[string]int)
	scenarioSet := make(map[string]bool)
	regionSet := make(map[string]bool)
	errors := make([]ErrorInfo, 0)

	for i := range m.activeRuns {
		run := &m.activeRuns[i]
		providerCounts[run.Provider]++
		scenarioSet[run.Scenario] = true
		regionSet[run.Region] = true

		if run.Status == StatusFailed {
			errors = append(errors, ErrorInfo{
				RunID:    run.RunID,
				Scenario: run.Scenario,
				Provider: run.Provider,
				Region:   run.Region,
				Error:    run.Error,
			})
		}
	}

	regions := make([]string, 0, len(regionSet))
	for region := range regionSet {
		regions = append(regions, region)
	}

	return &Summary{
		TotalRuns:      m.totalRuns,
		SuccessCount:   m.successCount,
		FailedCount:    m.failedCount,
		TotalCost:      m.totalCost,
		TotalTokens:    m.totalTokens,
		TotalDuration:  time.Since(m.startTime),
		AvgDuration:    avgDuration,
		ProviderCounts: providerCounts,
		ScenarioCount:  len(scenarioSet),
		Regions:        regions,
		Errors:         errors,
		OutputDir:      outputDir,
		HTMLReport:     htmlReport,
	}
}

// buildSummaryFromStateStore builds summary using persisted run results (assertions, tool stats).
func (m *Model) buildSummaryFromStateStore(outputDir, htmlReport string) *Summary {
	ctx := context.Background()

	totalRuns := len(m.activeRuns)
	success := 0
	failed := 0
	var totalCost float64
	var totalTokens int64
	var totalDuration time.Duration
	providerCounts := make(map[string]int)
	scenarioSet := make(map[string]bool)
	regionSet := make(map[string]bool)
	errors := make([]ErrorInfo, 0)
	assertTotal := 0
	assertFailed := 0

	for i := range m.activeRuns {
		run := &m.activeRuns[i]
		res, err := m.stateStore.GetResult(ctx, run.RunID)
		if err != nil {
			errors = append(errors, ErrorInfo{
				RunID:    run.RunID,
				Scenario: run.Scenario,
				Provider: run.Provider,
				Region:   run.Region,
				Error:    err.Error(),
			})
			failed++
			continue
		}

		providerCounts[res.ProviderID]++
		scenarioSet[res.ScenarioID] = true
		regionSet[res.Region] = true

		totalCost += res.Cost.TotalCost
		totalTokens += int64(res.Cost.InputTokens + res.Cost.OutputTokens + res.Cost.CachedTokens)
		totalDuration += res.Duration

		if res.Error != "" {
			failed++
			errors = append(errors, ErrorInfo{
				RunID:    res.RunID,
				Scenario: res.ScenarioID,
				Provider: res.ProviderID,
				Region:   res.Region,
				Error:    res.Error,
			})
		} else {
			success++
		}

		assertTotal += res.ConversationAssertions.Total
		assertFailed += res.ConversationAssertions.Failed
	}

	regions := make([]string, 0, len(regionSet))
	for region := range regionSet {
		regions = append(regions, region)
	}

	avgDuration := time.Duration(0)
	if success+failed > 0 {
		avgDuration = totalDuration / time.Duration(success+failed)
	}

	return &Summary{
		TotalRuns:       totalRuns,
		SuccessCount:    success,
		FailedCount:     failed,
		TotalCost:       totalCost,
		TotalTokens:     totalTokens,
		TotalDuration:   totalDuration,
		AvgDuration:     avgDuration,
		ProviderCounts:  providerCounts,
		ScenarioCount:   len(scenarioSet),
		Regions:         regions,
		Errors:          errors,
		OutputDir:       outputDir,
		HTMLReport:      htmlReport,
		AssertionTotal:  assertTotal,
		AssertionFailed: assertFailed,
	}
}

// NewModel creates a new TUI model with the specified configuration file and total run count.
func NewModel(configFile string, totalRuns int) *Model {
	width, height, supported, reason := CheckTerminalSize()

	// Ensure we always have minimum dimensions
	if width < MinTerminalWidth {
		width = MinTerminalWidth
	}
	if height < MinTerminalHeight {
		height = MinTerminalHeight
	}

	return &Model{
		configFile:     configFile,
		totalRuns:      totalRuns,
		startTime:      time.Now(),
		activeRuns:     make([]RunInfo, 0),
		logs:           make([]LogEntry, 0, maxLogBufferSize),
		width:          width,
		height:         height,
		isTUIMode:      supported,
		fallbackReason: reason,
		convPane:       NewConversationPane(),
		currentPage:    pageMain,
	}
}

// SetStateStore attaches a state store for building summaries from run results.
func (m *Model) SetStateStore(store runResultStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateStore = store
}

// Run starts the TUI application
func Run(ctx context.Context, model *Model) error {
	if !model.isTUIMode {
		return fmt.Errorf("TUI mode not supported: %s", model.fallbackReason)
	}

	p := tea.NewProgram(model)

	errCh := make(chan error, 1)
	go func() {
		if _, err := p.Run(); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		p.Quit()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// CheckTerminalSize checks if the terminal is large enough for TUI mode
func CheckTerminalSize() (width, height int, supported bool, reason string) {
	// Try stdout first (fd 1), then stderr (fd 2), then stdin (fd 0)
	// This ensures TUI works even when stdin/stdout are redirected
	for _, fd := range []int{1, 2, 0} {
		width, height, err := term.GetSize(fd)
		if err == nil {
			if width < MinTerminalWidth || height < MinTerminalHeight {
				return width, height, false, fmt.Sprintf(
					"terminal too small (%dx%d, minimum %dx%d required)",
					width,
					height,
					MinTerminalWidth,
					MinTerminalHeight,
				)
			}
			return width, height, true, ""
		}
	}

	return 0, 0, false, "unable to detect terminal size (not a TTY)"
}
