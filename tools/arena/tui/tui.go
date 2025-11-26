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
	}

	return m, nil
}

// View renders the TUI
func (m *Model) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isTUIMode {
		return ""
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
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Padding(0, 1)

	progress := fmt.Sprintf("[%d/%d Complete]", m.completedCount, m.totalRuns)
	timeStr := fmt.Sprintf("⏱ %s", formatDuration(elapsed))

	return style.Render(
		fmt.Sprintf("PromptArena - %s        %s %s", m.configFile, progress, timeStr),
	)
}

func (m *Model) renderActiveRuns() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1).
		Width(m.width - borderPadding)

	title := fmt.Sprintf("Active Runs (%d concurrent workers)", len(m.activeRuns))
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
		lines = append(lines, fmt.Sprintf("...and %d more", remaining))
	}

	return style.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) renderMetrics() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1).
		Width((m.width / panelDivisor) - borderPadding)

	avgDuration := time.Duration(0)
	if m.completedCount > 0 {
		avgDuration = m.totalDuration / time.Duration(m.completedCount)
	}

	lines := []string{
		"Metrics",
		"────────────────────",
		fmt.Sprintf("Completed:    %d/%d", m.completedCount, m.totalRuns),
		fmt.Sprintf("Success:      %d", m.successCount),
		fmt.Sprintf("Errors:       %d", m.failedCount),
		fmt.Sprintf("Total Cost:   $%.4f", m.totalCost),
		fmt.Sprintf("Total Tokens: %s", formatNumber(m.totalTokens)),
		fmt.Sprintf("Avg Duration: %s", formatDuration(avgDuration)),
		fmt.Sprintf("Workers:      %d/%d", len(m.activeRuns), len(m.activeRuns)),
	}

	return style.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) renderLogs() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1).
		Width((m.width / panelDivisor) - borderPadding)

	lines := []string{"Logs", "──────────────────────────"}

	startIdx := 0
	if len(m.logs) > maxLogLines {
		startIdx = len(m.logs) - maxLogLines
	}

	for i := startIdx; i < len(m.logs); i++ {
		log := m.logs[i]
		lines = append(lines, m.formatLogLine(log))
	}

	return style.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) formatRunLine(run *RunInfo) string {
	var status string
	var statusColor lipgloss.Color

	switch run.Status {
	case StatusRunning:
		status = "●"
		statusColor = lipgloss.Color("12")
	case StatusCompleted:
		status = "✓"
		statusColor = lipgloss.Color("10")
	case StatusFailed:
		status = "✗"
		statusColor = lipgloss.Color("9")
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
		levelColor = lipgloss.Color("12")
	case "WARN":
		levelColor = lipgloss.Color("11")
	case "ERROR":
		levelColor = lipgloss.Color("9")
	case "DEBUG":
		levelColor = lipgloss.Color("8")
	default:
		levelColor = lipgloss.Color("7")
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

// NewModel creates a new TUI model
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
