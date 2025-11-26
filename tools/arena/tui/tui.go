// Package tui provides a terminal user interface for PromptArena execution monitoring.
// It implements a multi-pane display showing active runs, metrics, and logs in real-time.
package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Terminal size requirements
const (
	MinTerminalWidth  = 80
	MinTerminalHeight = 24
)

// Display constants
const (
	borderPadding       = 4
	activeRunsHeight    = 10
	maxLogLines         = 7
	maxLogBufferSize    = 100
	tickIntervalMs      = 500
	durationPrecisionMs = 100
	numberSeparator     = 1000
	panelDivisor        = 2
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

	logs []LogEntry

	isTUIMode      bool
	fallbackReason string

	// Summary state
	summary     *Summary
	showSummary bool
}

// RunInfo tracks information about a single run
type RunInfo struct {
	RunID     string
	Scenario  string
	Provider  string
	Region    string
	Status    RunStatus
	Duration  time.Duration
	Cost      float64
	Error     string
	StartTime time.Time
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

// Update handles bubbletea messages and updates the model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tick()

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m, nil

	case RunStartedMsg:
		m.handleRunStarted(&msg)
		return m, nil

	case RunCompletedMsg:
		m.handleRunCompleted(&msg)
		return m, nil

	case RunFailedMsg:
		m.handleRunFailed(&msg)
		return m, nil

	case LogMsg:
		m.handleLogMsg(&msg)
		return m, nil

	case ShowSummaryMsg:
		m.handleShowSummary(&msg)
		return m, tea.Quit
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
	// In real usage, we'd use a timer, but for now keep completed runs visible
}

// handleRunFailed processes a run failed event from the observer.
// Note: Called with mutex already locked by Update.
func (m *Model) handleRunFailed(msg *RunFailedMsg) {
	// Find and update the run in activeRuns
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID == msg.RunID {
			m.activeRuns[i].Status = StatusFailed
			m.activeRuns[i].Error = msg.Error.Error()
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

// handleLogMsg processes a log message from the interceptor
func (m *Model) handleLogMsg(msg *LogMsg) {
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Timestamp,
		Level:     msg.Level,
		Message:   msg.Message,
	})
	m.trimLogs()
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

	elapsed := time.Since(m.startTime).Truncate(time.Second)

	header := m.renderHeader(elapsed)
	activeRuns := m.renderActiveRuns()
	metrics := m.renderMetrics()
	logs := m.renderLogs()

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, metrics, logs)

	return lipgloss.JoinVertical(lipgloss.Left, header, activeRuns, bottomRow)
}

func (m *Model) renderHeader(elapsed time.Duration) string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorPurple)).
		Background(lipgloss.Color(colorDarkBg)).
		Padding(0, 2).
		Width(m.width)

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorGreen)).
		Bold(true)

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorLightBlue))

	progress := progressStyle.Render(fmt.Sprintf("[%d/%d Complete]", m.completedCount, m.totalRuns))
	timeStr := timeStyle.Render(fmt.Sprintf("⏱  %s", formatDuration(elapsed)))

	headerText := fmt.Sprintf("PromptArena - %s    %s    %s", m.configFile, progress, timeStr)
	return headerStyle.Render(headerText)
}

func (m *Model) renderActiveRuns() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorIndigo)).
		Padding(1).
		Width(m.width - borderPadding)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorViolet))

	title := titleStyle.Render(fmt.Sprintf("Active Runs (%d concurrent workers)", len(m.activeRuns)))
	lines := []string{title, ""}

	maxLines := m.height - activeRunsHeight
	displayCount := len(m.activeRuns)
	if displayCount > maxLines {
		displayCount = maxLines
	}

	for i := 0; i < displayCount; i++ {
		run := m.activeRuns[i]
		lines = append(lines, m.formatRunLine(&run))
	}

	if len(m.activeRuns) > displayCount {
		remaining := len(m.activeRuns) - displayCount
		moreStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorGray)).Italic(true)
		lines = append(lines, moreStyle.Render(fmt.Sprintf("...and %d more", remaining)))
	}

	return boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) renderMetrics() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorGreen)).
		Padding(1).
		Width((m.width / panelDivisor) - borderPadding)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorEmerald))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorLightGray))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorWhite)).Bold(true)
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen)).Bold(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorRed)).Bold(true)
	costStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorYellow)).Bold(true)

	avgDuration := time.Duration(0)
	if m.completedCount > 0 {
		avgDuration = m.totalDuration / time.Duration(m.completedCount)
	}

	lines := []string{
		titleStyle.Render("📊 Metrics"),
		"",
		fmt.Sprintf("%s %s", labelStyle.Render("Completed:"), valueStyle.Render(fmt.Sprintf("%d/%d", m.completedCount, m.totalRuns))),
		fmt.Sprintf("%s %s", labelStyle.Render("Success:  "), successStyle.Render(fmt.Sprintf("%d", m.successCount))),
		fmt.Sprintf("%s %s", labelStyle.Render("Errors:   "), errorStyle.Render(fmt.Sprintf("%d", m.failedCount))),
		"",
		fmt.Sprintf("%s %s", labelStyle.Render("Total Cost:  "), costStyle.Render(fmt.Sprintf("$%.4f", m.totalCost))),
		fmt.Sprintf("%s %s", labelStyle.Render("Total Tokens:"), valueStyle.Render(formatNumber(m.totalTokens))),
		fmt.Sprintf("%s %s", labelStyle.Render("Avg Duration:"), valueStyle.Render(formatDuration(avgDuration))),
		fmt.Sprintf("%s %s", labelStyle.Render("Workers:     "), valueStyle.Render(fmt.Sprintf("%d", len(m.activeRuns)))),
	}

	return boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) renderLogs() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorLightBlue)).
		Padding(1).
		Width((m.width / panelDivisor) - borderPadding)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSky))
	lines := []string{titleStyle.Render("📝 Logs"), ""}

	startIdx := 0
	if len(m.logs) > maxLogLines {
		startIdx = len(m.logs) - maxLogLines
	}

	for i := startIdx; i < len(m.logs); i++ {
		log := m.logs[i]
		lines = append(lines, m.formatLogLine(log))
	}

	return boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) formatRunLine(run *RunInfo) string {
	var status string
	var statusColor lipgloss.Color

	switch run.Status {
	case StatusRunning:
		status = "●"
		statusColor = lipgloss.Color(colorBlue) // Blue for running
	case StatusCompleted:
		status = "✓"
		statusColor = lipgloss.Color(colorGreen) // Green for success
	case StatusFailed:
		status = "✗"
		statusColor = lipgloss.Color(colorRed) // Red for failure
	}

	statusStyle := lipgloss.NewStyle().Foreground(statusColor)
	runInfo := fmt.Sprintf("%s/%s/%s", run.Provider, run.Scenario, run.Region)

	switch run.Status {
	case StatusRunning:
		elapsed := time.Since(run.StartTime).Truncate(time.Millisecond * durationPrecisionMs)
		return fmt.Sprintf("[%s] %-40s ⏱ %s", statusStyle.Render(status), runInfo, formatDuration(elapsed))
	case StatusFailed:
		return fmt.Sprintf("[%s] %-40s ERROR", statusStyle.Render(status), runInfo)
	case StatusCompleted:
		return fmt.Sprintf(
			"[%s] %-40s ⏱ %s  $%.4f",
			statusStyle.Render(status),
			runInfo,
			formatDuration(run.Duration),
			run.Cost,
		)
	}
	return ""
}

func (m *Model) formatLogLine(log LogEntry) string {
	var levelColor lipgloss.Color
	switch log.Level {
	case "INFO":
		levelColor = lipgloss.Color(colorBlue) // Blue
	case "WARN":
		levelColor = lipgloss.Color(colorAmber) // Amber
	case "ERROR":
		levelColor = lipgloss.Color(colorRed) // Red
	case "DEBUG":
		levelColor = lipgloss.Color(colorGray) // Gray
	default:
		levelColor = lipgloss.Color(colorLightGray) // Light gray
	}

	levelStyle := lipgloss.NewStyle().Foreground(levelColor)
	return fmt.Sprintf("[%s] %s", levelStyle.Render(log.Level), log.Message)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Truncate(time.Millisecond * durationPrecisionMs).String()
}

func formatNumber(n int64) string {
	if n < numberSeparator {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%s,%03d", formatNumber(n/numberSeparator), n%numberSeparator)
}

func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*tickIntervalMs, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// CheckTerminalSize checks if the terminal is large enough for TUI mode
func CheckTerminalSize() (width, height int, supported bool, reason string) {
	fd := 0 // stdin
	width, height, err := term.GetSize(fd)
	if err != nil {
		return 0, 0, false, fmt.Sprintf("unable to detect terminal size: %v", err)
	}

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

// BuildSummary creates a Summary from the current model state with output directory and HTML report path.
func (m *Model) BuildSummary(outputDir, htmlReport string) *Summary {
	m.mu.Lock()
	defer m.mu.Unlock()

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

// NewModel creates a new TUI model with the specified configuration file and total run count.
func NewModel(configFile string, totalRuns int) *Model {
	width, height, supported, reason := CheckTerminalSize()

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
	}
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
