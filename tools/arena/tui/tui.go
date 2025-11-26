// Package tui provides a terminal user interface for PromptArena execution monitoring.
// It implements a multi-pane display showing active runs, metrics, and logs in real-time.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
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
	maxLogBufferSize    = 100
	tickIntervalMs      = 500
	durationPrecisionMs = 100
	numberSeparator     = 1000
	panelDivisor        = 2
	bannerLines         = 2  // Banner + info line (separator counted separately)
	bottomPanelPadding  = 4  // Border + padding
	metricsBoxWidth     = 40 // Fixed width for metrics box
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

	logs          []LogEntry
	logViewport   viewport.Model
	viewportReady bool

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
		m.mu.Unlock()
		return m, nil

	case tickMsg:
		return m, tick()

	case tea.KeyMsg:
		// Check for quit keys first
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyRunes:
			if len(msg.Runes) > 0 && msg.Runes[0] == 'q' {
				return m, tea.Quit
			}
		}

		// Let viewport handle scrolling keys (up/down arrows, pgup/pgdown, etc)
		if m.viewportReady {
			var cmd tea.Cmd
			m.logViewport, cmd = m.logViewport.Update(msg)
			return m, cmd
		}
		return m, nil

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

	elapsed := time.Since(m.startTime).Truncate(time.Second)

	header := m.renderHeader(elapsed)
	separator := lipgloss.NewStyle().Foreground(lipgloss.Color(colorGray)).Render(strings.Repeat("─", m.width))
	activeRuns := m.renderActiveRuns()
	metrics := m.renderMetrics()
	logs := m.renderLogs()

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, metrics, logs)

	// Add explicit newline at start to ensure header isn't cut off
	return "\n" + lipgloss.JoinVertical(lipgloss.Left, header, separator, activeRuns, separator, bottomRow)
}

func (m *Model) renderHeader(elapsed time.Duration) string {
	// Banner style
	bannerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorPurple)).
		Align(lipgloss.Center).
		Width(m.width)

	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorLightGray)).
		Align(lipgloss.Center).
		Width(m.width)

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorGreen)).
		Bold(true)

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorLightBlue))

	banner := bannerStyle.Render("✨ PromptArena ✨")
	progress := progressStyle.Render(fmt.Sprintf("[%d/%d Complete]", m.completedCount, m.totalRuns))
	timeStr := timeStyle.Render(fmt.Sprintf("⏱  %s", formatDuration(elapsed)))
	infoLine := infoStyle.Render(fmt.Sprintf("%s  •  %s  •  %s", filepath.Base(m.configFile), progress, timeStr))

	return lipgloss.JoinVertical(lipgloss.Left, banner, infoLine)
}

func (m *Model) renderActiveRuns() string {
	// Calculate available height for bottom panels - match the calculation in renderLogs
	availableHeight := m.height - bannerLines - 2
	if availableHeight < 10 {
		availableHeight = 10
	}
	// Active runs gets 60% of available space
	activeRunsHeight := int(float64(availableHeight) * 0.6)
	if activeRunsHeight < 8 {
		activeRunsHeight = 8
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorIndigo)).
		Padding(1).
		Width(m.width - borderPadding).
		MaxHeight(activeRunsHeight)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorViolet))

	title := titleStyle.Render(fmt.Sprintf("Active Runs (%d concurrent workers)", len(m.activeRuns)))
	lines := []string{title, ""}

	// Calculate max display lines (subtract title, padding, borders)
	maxLines := activeRunsHeight - bottomPanelPadding - 2
	if maxLines < 3 {
		maxLines = 3
	}

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
	// Calculate available height to match logs box
	availableHeight := m.height - bannerLines - 2
	if availableHeight < 10 {
		availableHeight = 10
	}
	activeRunsHeight := int(float64(availableHeight) * 0.6)
	metricsHeight := availableHeight - activeRunsHeight

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorGreen)).
		Padding(1).
		Width(metricsBoxWidth).
		MaxHeight(metricsHeight)

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
	// Calculate available height for bottom panels
	availableHeight := m.height - bannerLines - 2
	if availableHeight < 10 {
		availableHeight = 10
	}
	// Split remaining space with active runs (active runs gets 60%, logs/metrics get 40%)
	activeRunsHeight := int(float64(availableHeight) * 0.6)
	logsHeight := availableHeight - activeRunsHeight

	// Update viewport dimensions if needed
	if m.viewportReady {
		viewportHeight := logsHeight - bottomPanelPadding
		if viewportHeight < 3 {
			viewportHeight = 3
		}
		// Logs get remaining width after metrics box
		logsWidth := m.width - metricsBoxWidth - borderPadding - 2
		if logsWidth < 40 {
			logsWidth = 40 // Minimum width
		}
		m.logViewport.Width = logsWidth - 2 // -2 for padding
		m.logViewport.Height = viewportHeight

		// Update viewport content with all logs
		if len(m.logs) == 0 {
			m.logViewport.SetContent("No logs yet...")
		} else {
			logLines := make([]string, len(m.logs))
			for i, log := range m.logs {
				logLines[i] = m.formatLogLine(log)
			}
			m.logViewport.SetContent(strings.Join(logLines, "\n"))
		}
	}

	// Calculate logs box width (remaining space after metrics)
	logsWidth := m.width - metricsBoxWidth - borderPadding - 2
	if logsWidth < 40 {
		logsWidth = 40 // Minimum width
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorLightBlue)).
		Padding(1).
		Width(logsWidth)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSky))
	title := titleStyle.Render("📝 Logs (↑/↓ to scroll)")

	if !m.viewportReady {
		return boxStyle.Render(title + "\n\nInitializing...")
	}

	return boxStyle.Render(title + "\n" + m.logViewport.View())
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

// initViewport initializes the viewport for scrollable logs
func (m *Model) initViewport() {
	// Calculate initial dimensions
	availableHeight := m.height - bannerLines - 2
	if availableHeight < 10 {
		availableHeight = 10
	}
	activeRunsHeight := int(float64(availableHeight) * 0.6)
	logsHeight := availableHeight - activeRunsHeight
	viewportHeight := logsHeight - bottomPanelPadding
	if viewportHeight < 3 {
		viewportHeight = 3
	}

	m.logViewport = viewport.New(
		(m.width/panelDivisor)-borderPadding-2,
		viewportHeight,
	)
	m.logViewport.SetContent("Waiting for logs...")
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
